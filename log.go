package libcore

/*
   #cgo LDFLAGS: -landroid -llog

   #include <android/log.h>
   #include <string.h>
   #include <stdlib.h>
*/
import "C"
import (
	"bufio"
	"fmt"
	"github.com/sirupsen/logrus"
	"log"
	"os"
	"strings"
	"syscall"
	"unsafe"

	appLog "github.com/v2fly/v2ray-core/v4/app/log"
	commonLog "github.com/v2fly/v2ray-core/v4/common/log"
)

var (
	tag      = C.CString("libcore")
	tagV2Ray = C.CString("v2ray-core")
)

var levels = []logrus.Level{
	logrus.PanicLevel,
	logrus.FatalLevel,
	logrus.ErrorLevel,
	logrus.WarnLevel,
	logrus.InfoLevel,
	logrus.DebugLevel,
}

type androidHook struct {
}

type androidFormatter struct{}

func (f *androidFormatter) Format(entry *logrus.Entry) ([]byte, error) {

	msgWithLevel := fmt.Sprint("[", strings.Title(entry.Level.String()), "] ", entry.Message)
	return []byte(msgWithLevel), nil
}

func (hook *androidHook) Levels() []logrus.Level {
	return levels
}

func (hook *androidHook) Fire(e *logrus.Entry) error {
	var priority C.int

	formatted, err := logrus.StandardLogger().Formatter.Format(e)
	if err != nil {
		return err
	}
	str := C.CString(string(formatted))

	switch e.Level {
	case logrus.PanicLevel:
		priority = C.ANDROID_LOG_FATAL
	case logrus.FatalLevel:
		priority = C.ANDROID_LOG_FATAL
	case logrus.ErrorLevel:
		priority = C.ANDROID_LOG_ERROR
	case logrus.WarnLevel:
		priority = C.ANDROID_LOG_WARN
	case logrus.InfoLevel:
		priority = C.ANDROID_LOG_INFO
	case logrus.DebugLevel:
		priority = C.ANDROID_LOG_DEBUG
	}
	C.__android_log_write(priority, tag, str)
	C.free(unsafe.Pointer(str))
	return nil
}

type v2rayLogWriter struct {
}

func (w *v2rayLogWriter) Write(s string) error {
	str := C.CString(s)
	C.__android_log_write(C.ANDROID_LOG_DEBUG, tagV2Ray, str)
	C.free(unsafe.Pointer(str))
	return nil
}

func (w *v2rayLogWriter) Close() error {
	return nil
}

type stdLogWriter struct{}

func (stdLogWriter) Write(p []byte) (n int, err error) {
	str := C.CString(string(p))
	C.__android_log_write(C.ANDROID_LOG_INFO, tag, str)
	C.free(unsafe.Pointer(str))
	return len(p), nil
}

func lineLog(f *os.File, priority C.int) {
	const logSize = 1024 // matches android/log.h.
	r := bufio.NewReaderSize(f, logSize)
	for {
		line, _, err := r.ReadLine()
		str := string(line)
		if err != nil {
			str += " " + err.Error()
		}
		cstr := C.CString(str)
		C.__android_log_write(priority, tag, cstr)
		C.free(unsafe.Pointer(cstr))
		if err != nil {
			break
		}
	}
}

func initLog() {
	log.SetOutput(stdLogWriter{})
	log.SetFlags(log.Flags() &^ log.LstdFlags)

	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	stderr := w
	if err := syscall.Dup3(int(w.Fd()), int(os.Stderr.Fd()), 0); err != nil {
		panic(err)
	}
	go lineLog(r, C.ANDROID_LOG_ERROR)

	r, w, err = os.Pipe()
	if err != nil {
		panic(err)
	}
	stdout := w
	if err := syscall.Dup3(int(w.Fd()), int(os.Stdout.Fd()), 0); err != nil {
		panic(err)
	}
	go lineLog(r, C.ANDROID_LOG_INFO)

	os.Stderr = stderr
	os.Stdout = stdout

	logrus.SetFormatter(&androidFormatter{})
	logrus.AddHook(&androidHook{})

	_ = appLog.RegisterHandlerCreator(appLog.LogType_Console, func(lt appLog.LogType,
		options appLog.HandlerCreatorOptions) (commonLog.Handler, error) {
		return commonLog.NewLogger(func() commonLog.Writer {
			return &v2rayLogWriter{}
		}), nil
	})
}
