package store

import (
	"sync"
	"time"
)

type MessageAction string

const (
	ActionSend    MessageAction = "send"
	ActionReceive MessageAction = "receive"
	ActionDelete  MessageAction = "delete"
)

type Message struct {
	ID            string            `json:"id"`
	MessageID     string            `json:"messageId"`
	ReceiptHandle string            `json:"receiptHandle,omitempty"`
	QueueURL      string            `json:"queueUrl"`
	QueueName     string            `json:"queueName"`
	Body          string            `json:"body"`
	Attributes    map[string]string `json:"attributes,omitempty"`
	Action        MessageAction     `json:"action"`
	Timestamp     time.Time         `json:"timestamp"`
	Deleted       bool              `json:"deleted"`
	DeletedAt     *time.Time        `json:"deletedAt,omitempty"`
}

type QueueStats struct {
	QueueName     string `json:"queueName"`
	QueueURL      string `json:"queueUrl"`
	TotalSent     int    `json:"totalSent"`
	TotalReceived int    `json:"totalReceived"`
	TotalDeleted  int    `json:"totalDeleted"`
	Pending       int    `json:"pending"`
}

type Store struct {
	mu       sync.RWMutex
	messages map[string]*Message          // messageId -> Message
	queues   map[string]map[string]bool   // queueName -> messageIds
	history  []*Message                   // chronological history
	receipts map[string]string            // receiptHandle -> messageId
}

func New() *Store {
	return &Store{
		messages: make(map[string]*Message),
		queues:   make(map[string]map[string]bool),
		history:  make([]*Message, 0),
		receipts: make(map[string]string),
	}
}

func (s *Store) RecordSend(queueURL, queueName, messageID, body string, attributes map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msg := &Message{
		ID:         generateID(),
		MessageID:  messageID,
		QueueURL:   queueURL,
		QueueName:  queueName,
		Body:       body,
		Attributes: attributes,
		Action:     ActionSend,
		Timestamp:  time.Now(),
	}

	s.messages[messageID] = msg
	if s.queues[queueName] == nil {
		s.queues[queueName] = make(map[string]bool)
	}
	s.queues[queueName][messageID] = true
	s.history = append(s.history, msg)
}

func (s *Store) RecordReceive(queueURL, queueName, messageID, receiptHandle, body string, attributes map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create receive event
	event := &Message{
		ID:            generateID(),
		MessageID:     messageID,
		ReceiptHandle: receiptHandle,
		QueueURL:      queueURL,
		QueueName:     queueName,
		Body:          body,
		Attributes:    attributes,
		Action:        ActionReceive,
		Timestamp:     time.Now(),
	}
	s.history = append(s.history, event)

	// Track receipt handle for deletion lookup
	s.receipts[receiptHandle] = messageID

	// If we haven't seen this message before (e.g., pre-existing in queue), add it
	if _, exists := s.messages[messageID]; !exists {
		msg := &Message{
			ID:            event.ID,
			MessageID:     messageID,
			ReceiptHandle: receiptHandle,
			QueueURL:      queueURL,
			QueueName:     queueName,
			Body:          body,
			Attributes:    attributes,
			Action:        ActionReceive,
			Timestamp:     time.Now(),
		}
		s.messages[messageID] = msg
		if s.queues[queueName] == nil {
			s.queues[queueName] = make(map[string]bool)
		}
		s.queues[queueName][messageID] = true
	}
}

func (s *Store) RecordDelete(queueURL, queueName, receiptHandle string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	// Create delete event
	event := &Message{
		ID:            generateID(),
		ReceiptHandle: receiptHandle,
		QueueURL:      queueURL,
		QueueName:     queueName,
		Action:        ActionDelete,
		Timestamp:     now,
	}

	// Try to find the message by receipt handle
	if messageID, ok := s.receipts[receiptHandle]; ok {
		event.MessageID = messageID
		if msg, exists := s.messages[messageID]; exists {
			msg.Deleted = true
			msg.DeletedAt = &now
			event.Body = msg.Body
		}
	}

	s.history = append(s.history, event)
}

func (s *Store) GetMessages(queueName string, includeDeleted bool) []*Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Message
	for _, msg := range s.messages {
		if queueName != "" && msg.QueueName != queueName {
			continue
		}
		if !includeDeleted && msg.Deleted {
			continue
		}
		result = append(result, msg)
	}
	return result
}

func (s *Store) GetHistory(limit int) []*Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > len(s.history) {
		limit = len(s.history)
	}

	// Return most recent first
	result := make([]*Message, limit)
	for i := 0; i < limit; i++ {
		result[i] = s.history[len(s.history)-1-i]
	}
	return result
}

func (s *Store) GetQueueStats() []QueueStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := make(map[string]*QueueStats)

	for _, event := range s.history {
		if stats[event.QueueName] == nil {
			stats[event.QueueName] = &QueueStats{
				QueueName: event.QueueName,
				QueueURL:  event.QueueURL,
			}
		}
		switch event.Action {
		case ActionSend:
			stats[event.QueueName].TotalSent++
		case ActionReceive:
			stats[event.QueueName].TotalReceived++
		case ActionDelete:
			stats[event.QueueName].TotalDeleted++
		}
	}

	// Calculate pending (sent but not deleted)
	for queueName, queueMsgs := range s.queues {
		if stats[queueName] == nil {
			continue
		}
		pending := 0
		for msgID := range queueMsgs {
			if msg, ok := s.messages[msgID]; ok && !msg.Deleted {
				pending++
			}
		}
		stats[queueName].Pending = pending
	}

	result := make([]QueueStats, 0, len(stats))
	for _, s := range stats {
		result = append(result, *s)
	}
	return result
}

func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = make(map[string]*Message)
	s.queues = make(map[string]map[string]bool)
	s.history = make([]*Message, 0)
	s.receipts = make(map[string]string)
}

var idCounter int64
var idMu sync.Mutex

func generateID() string {
	idMu.Lock()
	defer idMu.Unlock()
	idCounter++
	return time.Now().Format("20060102150405") + "-" + string(rune(idCounter))
}
