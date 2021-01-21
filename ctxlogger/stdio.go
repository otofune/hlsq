package ctxlogger

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"time"
)

type stdIOLogger struct {
	stderr *log.Logger
	stdout *log.Logger
}

// NewStdIOLogger logger that output to stdio
func NewStdIOLogger() Logger {
	return &stdIOLogger{
		stdout: log.New(os.Stdout, "", 0),
		stderr: log.New(os.Stderr, "", 0),
	}
}

func (l *stdIOLogger) calleeLine() string {
	_, file, line, ok := runtime.Caller(3)
	if !ok {
		return "???:???"
	}
	filePart := strings.Split(file, "/")
	return fmt.Sprintf("%s:%d", filePart[len(filePart)-1], line)
}

func (l *stdIOLogger) withCommonPrefix(log func(format string, v ...interface{}), format string, args ...interface{}) {
	ts := time.Now().Format("2006-01-02 15:04:05")
	margs := []interface{}{ts, l.calleeLine()}
	margs = append(margs, args...)
	log("%s %s: "+format, margs...)
}

func (l *stdIOLogger) Debugf(format string, args ...interface{}) {
	l.withCommonPrefix(l.stdout.Printf, format, args...)
}

func (l *stdIOLogger) Printf(format string, args ...interface{}) {
	l.withCommonPrefix(l.stdout.Printf, format, args...)
}

func (l *stdIOLogger) Errorf(format string, args ...interface{}) {
	l.withCommonPrefix(l.stderr.Printf, format, args...)
}

func (l *stdIOLogger) Fatalf(format string, args ...interface{}) {
	l.withCommonPrefix(l.stderr.Fatalf, format, args...)
}
