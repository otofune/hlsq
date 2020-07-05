package logger

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"time"
)

type StdIOLogger struct {
	stderr *log.Logger
	stdout *log.Logger
}

func NewStdIOLogger() *StdIOLogger {
	return &StdIOLogger{
		stdout: log.New(os.Stdout, "", 0),
		stderr: log.New(os.Stderr, "", 0),
	}
}

func (l *StdIOLogger) calleeLine() string {
	_, file, line, ok := runtime.Caller(3)
	if !ok {
		return "???:???"
	}
	filePart := strings.Split(file, "/")
	return fmt.Sprintf("%s:%d", filePart[len(filePart)-1], line)
}

func (l *StdIOLogger) withCommonPrefix(log func(format string, v ...interface{}), format string, args ...interface{}) {
	ts := time.Now().Format("2006-01-02 15:04:05")
	margs := []interface{}{l.calleeLine(), ts}
	margs = append(margs, args...)
	log("[%s] %s "+format, margs...)
}

func (l *StdIOLogger) Debugf(format string, args ...interface{}) {
	l.withCommonPrefix(l.stdout.Printf, format, args...)
}

func (l *StdIOLogger) Printf(format string, args ...interface{}) {
	l.withCommonPrefix(l.stdout.Printf, format, args...)
}

func (l *StdIOLogger) Errorf(format string, args ...interface{}) {
	l.withCommonPrefix(l.stderr.Printf, format, args...)
}

func (l *StdIOLogger) Fatalf(format string, args ...interface{}) {
	l.withCommonPrefix(l.stderr.Fatalf, format, args...)
}
