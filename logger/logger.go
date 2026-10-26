package logger

// A Logger represents an active logging object that generates
// lines of output to an io.Writer.
type Logger interface {
	Debug(args ...interface{})
	Debugf(format string, args ...interface{})
	Debugln(args ...interface{})

	Error(args ...interface{})
	Errorf(format string, args ...interface{})
	Errorln(args ...interface{})

	Info(args ...interface{})
	Infof(format string, args ...interface{})
	Infoln(args ...interface{})

	Trace(args ...interface{})
	Tracef(format string, args ...interface{})
	Traceln(args ...interface{})

	Warn(args ...interface{})
	Warnf(format string, args ...interface{})
	Warnln(args ...interface{})
}

// Default returns the default logger.
var Default = Discard()

// Discard returns a no-op logger
func Discard() Logger {
	return &discard{}
}

type discard struct{}

func (*discard) Debug(...interface{})                   {}
func (*discard) Debugf(string, ...interface{})          {}
func (*discard) Debugln(...interface{})                 {}
func (*discard) Error(...interface{})                   {}
func (*discard) Errorf(string, ...interface{})          {}
func (*discard) Errorln(...interface{})                 {}
func (*discard) Info(...interface{})                    {}
func (*discard) Infof(string, ...interface{})           {}
func (*discard) Infoln(...interface{})                  {}
func (*discard) Trace(...interface{})                   {}
func (*discard) Tracef(string, ...interface{})          {}
func (*discard) Traceln(...interface{})                 {}
func (*discard) Warn(...interface{})                    {}
func (*discard) Warnf(string, ...interface{})           {}
func (*discard) Warnln(...interface{})                  {}
func (d *discard) WithError(error) Logger               { return d }
func (d *discard) WithField(string, interface{}) Logger { return d }
