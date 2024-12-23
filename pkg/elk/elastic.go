package elk

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-logr/logr"
	"github.com/olivere/elastic/v7"
)

// ElasticsearchLogger implements the logr.LogSink interface
type ElasticsearchLogger struct {
	client    *elastic.Client
	indexName string
	name      string
	values    []interface{}
}

// NewElasticsearchLogger creates a new ElasticsearchLogger
func NewElasticsearchLogger(esURL, indexName string) (logr.Logger, error) {
	client, err := elastic.NewClient(
		elastic.SetURL(esURL),
		elastic.SetSniff(false), // Disable sniffing in single-node clusters
	)
	if err != nil {
		return logr.Logger{}, err
	}

	// Create the LogSink and wrap it in a logr.Logger
	logger := &ElasticsearchLogger{
		client:    client,
		indexName: indexName,
		name:      "",
		values:    nil,
	}
	return logr.New(logger), nil
}

// Init initializes the logger with optional configuration
func (l *ElasticsearchLogger) Init(info logr.RuntimeInfo) {
	// This can be used for runtime-specific configuration if needed.
	// For now, it can remain a no-op.
}

// Enabled determines whether a log level is enabled
func (l *ElasticsearchLogger) Enabled(level int) bool {
	// Enable all log levels for simplicity. Customize as needed.
	return true
}

// Info logs non-error messages
func (l *ElasticsearchLogger) Info(level int, msg string, kvPairs ...interface{}) {
	l.logMessage(level, msg, kvPairs...)
}

// Error logs error messages
func (l *ElasticsearchLogger) Error(err error, msg string, kvPairs ...interface{}) {
	logData := map[string]interface{}{
		"timestamp": time.Now(),
		"level":     "error",
		"logger":    l.name,
		"message":   msg,
		"error":     err.Error(),
		"values":    append(l.values, kvPairs...),
	}

	_, _ = l.client.Index().
		Index(l.indexName).
		BodyJson(logData).
		Do(context.Background())
}

// WithName adds a name to the logger
func (l *ElasticsearchLogger) WithName(name string) logr.LogSink {
	return &ElasticsearchLogger{
		client:    l.client,
		indexName: l.indexName,
		name:      name,
		values:    l.values,
	}
}

// WithValues adds key-value pairs to the logger
func (l *ElasticsearchLogger) WithValues(kvPairs ...interface{}) logr.LogSink {
	return &ElasticsearchLogger{
		client:    l.client,
		indexName: l.indexName,
		name:      l.name,
		values:    append(l.values, kvPairs...),
	}
}

func (l *ElasticsearchLogger) logMessage(level int, msg string, kvPairs ...interface{}) {
	// Convert kvPairs to a JSON string
	valuesJSON, err := json.Marshal(kvPairs)
	if err != nil {
		valuesJSON = []byte("null") // Fallback to null if serialization fails
	}

	logData := map[string]interface{}{
		"timestamp": time.Now(),
		"level":     level,
		"logger":    l.name,
		"message":   msg,
		"values":    string(valuesJSON), // Store as a JSON string
	}

	_, err = l.client.Index().
		Index(l.indexName).
		BodyJson(logData).
		Do(context.Background())
	if err != nil {
		// Handle logging errors
		panic(err)
	}
}
