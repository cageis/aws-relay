package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"

	"aws-relay/internal/store"
)

type Proxy struct {
	upstream *url.URL
	proxy    *httputil.ReverseProxy
	store    *store.Store
}

func New(upstreamURL string, s *store.Store) *Proxy {
	upstream, err := url.Parse(upstreamURL)
	if err != nil {
		log.Fatalf("Invalid upstream URL: %v", err)
	}

	p := &Proxy{
		upstream: upstream,
		store:    s,
	}

	p.proxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = upstream.Scheme
			req.URL.Host = upstream.Host
			req.Host = upstream.Host
		},
		ModifyResponse: p.modifyResponse,
	}

	return p
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Read and buffer the request body for inspection
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	// Store request body and content type in headers for response handling
	r.Header.Set("X-SQS-Relay-Request-Body", string(body))
	r.Header.Set("X-SQS-Relay-Content-Type", r.Header.Get("Content-Type"))
	r.Header.Set("X-SQS-Relay-Amz-Target", r.Header.Get("X-Amz-Target"))

	// Log the action
	action := p.parseAction(r, string(body))
	queueURL := p.parseQueueURL(r, string(body))
	log.Printf("[%s] %s %s", action, r.Method, queueURL)

	p.proxy.ServeHTTP(w, r)
}

func (p *Proxy) modifyResponse(resp *http.Response) error {
	// Get original request info
	reqBody := resp.Request.Header.Get("X-SQS-Relay-Request-Body")
	contentType := resp.Request.Header.Get("X-SQS-Relay-Content-Type")
	amzTarget := resp.Request.Header.Get("X-SQS-Relay-Amz-Target")
	resp.Request.Header.Del("X-SQS-Relay-Request-Body")
	resp.Request.Header.Del("X-SQS-Relay-Content-Type")
	resp.Request.Header.Del("X-SQS-Relay-Amz-Target")

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))

	isJSON := strings.Contains(contentType, "json")
	action := parseActionFromTarget(amzTarget)
	if action == "" {
		action = parseActionFromForm(reqBody)
	}

	queueURL := ""
	if isJSON {
		queueURL = parseJSONField(reqBody, "QueueUrl")
	} else {
		queueURL = parseFormField(reqBody, "QueueUrl")
	}
	queueName := extractQueueName(queueURL)

	switch action {
	case "SendMessage":
		p.handleSendMessage(queueURL, queueName, reqBody, string(body), isJSON)
	case "SendMessageBatch":
		p.handleSendMessageBatch(queueURL, queueName, reqBody, string(body), isJSON)
	case "ReceiveMessage":
		p.handleReceiveMessage(queueURL, queueName, string(body), isJSON)
	case "DeleteMessage":
		p.handleDeleteMessage(queueURL, queueName, reqBody, isJSON)
	case "DeleteMessageBatch":
		p.handleDeleteMessageBatch(queueURL, queueName, reqBody, isJSON)
	}

	return nil
}

func (p *Proxy) parseAction(r *http.Request, body string) string {
	// Try X-Amz-Target header first (JSON API)
	if target := r.Header.Get("X-Amz-Target"); target != "" {
		action := parseActionFromTarget(target)
		if action != "" {
			return action
		}
	}
	// Fall back to form-encoded Action parameter
	return parseActionFromForm(body)
}

func (p *Proxy) parseQueueURL(r *http.Request, body string) string {
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "json") {
		return parseJSONField(body, "QueueUrl")
	}
	return parseFormField(body, "QueueUrl")
}

func parseActionFromTarget(target string) string {
	// X-Amz-Target format: "AmazonSQS.SendMessage"
	if strings.HasPrefix(target, "AmazonSQS.") {
		return strings.TrimPrefix(target, "AmazonSQS.")
	}
	return ""
}

func parseActionFromForm(body string) string {
	re := regexp.MustCompile(`Action=([^&]+)`)
	matches := re.FindStringSubmatch(body)
	if len(matches) > 1 {
		return matches[1]
	}
	return "Unknown"
}

func parseFormField(body, field string) string {
	re := regexp.MustCompile(field + `=([^&]+)`)
	matches := re.FindStringSubmatch(body)
	if len(matches) > 1 {
		decoded, _ := url.QueryUnescape(matches[1])
		return decoded
	}
	return ""
}

func parseJSONField(body, field string) string {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return ""
	}
	if val, ok := data[field].(string); ok {
		return val
	}
	return ""
}

func (p *Proxy) handleSendMessage(queueURL, queueName, reqBody, respBody string, isJSON bool) {
	var msgBody, messageID string

	if isJSON {
		msgBody = parseJSONField(reqBody, "MessageBody")
		messageID = parseJSONField(respBody, "MessageId")
	} else {
		msgBody = parseFormField(reqBody, "MessageBody")
		messageID = extractXMLTag(respBody, "MessageId")
	}

	attrs := extractMessageAttributes(reqBody, isJSON)

	if messageID != "" {
		p.store.RecordSend(queueURL, queueName, messageID, msgBody, attrs)
		log.Printf("  -> Sent message %s to %s", messageID, queueName)
	}
}

func (p *Proxy) handleSendMessageBatch(queueURL, queueName, reqBody, respBody string, isJSON bool) {
	var messageIDs []string

	if isJSON {
		var resp map[string]interface{}
		if err := json.Unmarshal([]byte(respBody), &resp); err == nil {
			if successful, ok := resp["Successful"].([]interface{}); ok {
				for _, s := range successful {
					if entry, ok := s.(map[string]interface{}); ok {
						if id, ok := entry["MessageId"].(string); ok {
							messageIDs = append(messageIDs, id)
						}
					}
				}
			}
		}
	} else {
		messageIDs = extractAllXMLTags(respBody, "MessageId")
	}

	for _, messageID := range messageIDs {
		p.store.RecordSend(queueURL, queueName, messageID, "[batch message]", nil)
		log.Printf("  -> Sent batch message %s to %s", messageID, queueName)
	}
}

func (p *Proxy) handleReceiveMessage(queueURL, queueName, respBody string, isJSON bool) {
	var messages []receivedMessage

	if isJSON {
		messages = parseReceiveMessageResponseJSON(respBody)
	} else {
		messages = parseReceiveMessageResponseXML(respBody)
	}

	for _, msg := range messages {
		p.store.RecordReceive(queueURL, queueName, msg.MessageID, msg.ReceiptHandle, msg.Body, msg.Attributes)
		log.Printf("  <- Received message %s from %s", msg.MessageID, queueName)
	}
}

func (p *Proxy) handleDeleteMessage(queueURL, queueName, reqBody string, isJSON bool) {
	var receiptHandle string
	if isJSON {
		receiptHandle = parseJSONField(reqBody, "ReceiptHandle")
	} else {
		receiptHandle = parseFormField(reqBody, "ReceiptHandle")
	}

	if receiptHandle != "" {
		p.store.RecordDelete(queueURL, queueName, receiptHandle)
		log.Printf("  X Deleted message from %s", queueName)
	}
}

func (p *Proxy) handleDeleteMessageBatch(queueURL, queueName, reqBody string, isJSON bool) {
	if isJSON {
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(reqBody), &data); err == nil {
			if entries, ok := data["Entries"].([]interface{}); ok {
				for _, e := range entries {
					if entry, ok := e.(map[string]interface{}); ok {
						if rh, ok := entry["ReceiptHandle"].(string); ok {
							p.store.RecordDelete(queueURL, queueName, rh)
							log.Printf("  X Deleted batch message from %s", queueName)
						}
					}
				}
			}
		}
	} else {
		re := regexp.MustCompile(`DeleteMessageBatchRequestEntry\.\d+\.ReceiptHandle=([^&]+)`)
		matches := re.FindAllStringSubmatch(reqBody, -1)
		for _, match := range matches {
			if len(match) > 1 {
				receiptHandle, _ := url.QueryUnescape(match[1])
				p.store.RecordDelete(queueURL, queueName, receiptHandle)
				log.Printf("  X Deleted batch message from %s", queueName)
			}
		}
	}
}

func extractQueueName(queueURL string) string {
	parts := strings.Split(queueURL, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return queueURL
}

func extractXMLTag(xml, tag string) string {
	re := regexp.MustCompile(`<` + tag + `>([^<]+)</` + tag + `>`)
	matches := re.FindStringSubmatch(xml)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func extractAllXMLTags(xml, tag string) []string {
	re := regexp.MustCompile(`<` + tag + `>([^<]+)</` + tag + `>`)
	matches := re.FindAllStringSubmatch(xml, -1)
	var results []string
	for _, match := range matches {
		if len(match) > 1 {
			results = append(results, match[1])
		}
	}
	return results
}

func extractMessageAttributes(body string, isJSON bool) map[string]string {
	attrs := make(map[string]string)

	if isJSON {
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(body), &data); err == nil {
			if msgAttrs, ok := data["MessageAttributes"].(map[string]interface{}); ok {
				for name, v := range msgAttrs {
					if attr, ok := v.(map[string]interface{}); ok {
						if sv, ok := attr["StringValue"].(string); ok {
							attrs[name] = sv
						}
					}
				}
			}
		}
	} else {
		nameRe := regexp.MustCompile(`MessageAttribute\.(\d+)\.Name=([^&]+)`)
		valueRe := regexp.MustCompile(`MessageAttribute\.(\d+)\.Value\.StringValue=([^&]+)`)

		names := make(map[string]string)
		values := make(map[string]string)

		for _, match := range nameRe.FindAllStringSubmatch(body, -1) {
			if len(match) > 2 {
				decoded, _ := url.QueryUnescape(match[2])
				names[match[1]] = decoded
			}
		}

		for _, match := range valueRe.FindAllStringSubmatch(body, -1) {
			if len(match) > 2 {
				decoded, _ := url.QueryUnescape(match[2])
				values[match[1]] = decoded
			}
		}

		for idx, name := range names {
			if val, ok := values[idx]; ok {
				attrs[name] = val
			}
		}
	}

	return attrs
}

type receivedMessage struct {
	MessageID     string
	ReceiptHandle string
	Body          string
	Attributes    map[string]string
}

func parseReceiveMessageResponseJSON(body string) []receivedMessage {
	var messages []receivedMessage

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return messages
	}

	msgList, ok := resp["Messages"].([]interface{})
	if !ok {
		return messages
	}

	for _, m := range msgList {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}

		rm := receivedMessage{
			Attributes: make(map[string]string),
		}

		if id, ok := msg["MessageId"].(string); ok {
			rm.MessageID = id
		}
		if rh, ok := msg["ReceiptHandle"].(string); ok {
			rm.ReceiptHandle = rh
		}
		if b, ok := msg["Body"].(string); ok {
			rm.Body = b
		}

		if attrs, ok := msg["MessageAttributes"].(map[string]interface{}); ok {
			for name, v := range attrs {
				if attr, ok := v.(map[string]interface{}); ok {
					if sv, ok := attr["StringValue"].(string); ok {
						rm.Attributes[name] = sv
					}
				}
			}
		}

		messages = append(messages, rm)
	}

	return messages
}

func parseReceiveMessageResponseXML(xml string) []receivedMessage {
	var messages []receivedMessage

	msgRe := regexp.MustCompile(`(?s)<Message>(.*?)</Message>`)
	msgMatches := msgRe.FindAllStringSubmatch(xml, -1)

	for _, match := range msgMatches {
		if len(match) > 1 {
			msgXML := match[1]
			msg := receivedMessage{
				MessageID:     extractXMLTag(msgXML, "MessageId"),
				ReceiptHandle: extractXMLTag(msgXML, "ReceiptHandle"),
				Body:          extractXMLTag(msgXML, "Body"),
				Attributes:    make(map[string]string),
			}

			attrRe := regexp.MustCompile(`(?s)<MessageAttribute>(.*?)</MessageAttribute>`)
			attrMatches := attrRe.FindAllStringSubmatch(msgXML, -1)
			for _, attrMatch := range attrMatches {
				if len(attrMatch) > 1 {
					name := extractXMLTag(attrMatch[1], "Name")
					value := extractXMLTag(attrMatch[1], "StringValue")
					if name != "" {
						msg.Attributes[name] = value
					}
				}
			}

			messages = append(messages, msg)
		}
	}

	return messages
}
