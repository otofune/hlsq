package logger

type dummyLogger struct{}

// NewDummyLogger creates logger that do nothing
func NewDummyLogger() Logger {
	return dummyLogger{}
}

func (dummyLogger) Debugf(format string, args ...interface{}) {}
func (dummyLogger) Printf(format string, args ...interface{}) {}
func (dummyLogger) Errorf(format string, args ...interface{}) {}
func (dummyLogger) Fatalf(format string, args ...interface{}) {}
