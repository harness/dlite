package logger

import "github.com/sirupsen/logrus"

// Logrus returns a Logger that wraps a logrus.Entry.
func Logrus(entry *logrus.Entry) Logger {
	return &wrapLogrus{entry}
}

type wrapLogrus struct {
	*logrus.Entry
}

func (w *wrapLogrus) WithError(err error) Logger {
	return &wrapLogrus{w.Entry.WithError(err)}
}

func (w *wrapLogrus) WithField(key string, value interface{}) Logger {
	return &wrapLogrus{w.Entry.WithField(key, value)}
}
