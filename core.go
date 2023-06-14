package noob

import (
	"bytes"
	"fmt"
	"github.com/dimasbagussusilo/nb-go-parser"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Ctx struct {
	startTime time.Time
	Provider  *HTTPProviderCtx

	Listener net.Listener
	*Router
}

// Start will run the Core & start serving the application
func (co *Ctx) Start() error {
	// Restart logger to re-read env
	restartLogger()

	var (
		e error
	)

	cfg := DefaultCfg

	crs := new(cors)

	// Prepare handlers for no route
	middlewares := []HandlerFunc{handleRequestLogger(log), crs.HandleCORS, HandleThrottling(), HandleTimeout}

	co.USE(middlewares...)
	// Handle root
	co.GET("/", HandleAPIStatus)

	// handler for not found page
	middlewares = append(middlewares, HandleNotFound)
	co.Provider.Engine.NoRoute(NewHandlerChain(middlewares).compact())

	hostInfo := cfg.Host
	if hostInfo == "" {
		hostInfo = "http://localhost"
	}

	// use listener if listener not nil
	if co.Listener != nil && cfg.UseListener {
		url := fmt.Sprintf("%s%s", co.Listener.Addr().String(), cfg.Path)
		log.Info(fmt.Sprintf("TimeToBoot = %s Running: Address = '%s'", time.Since(co.startTime).String(), url), map[string]interface{}{
			"address": url,
		})

		e = co.Provider.Engine.RunListener(co.Listener)
	} else {
		baseUrlInfo := fmt.Sprintf("%s:%d", hostInfo, cfg.Port)
		url := fmt.Sprintf("%s%s", baseUrlInfo, cfg.Path)
		log.Info(fmt.Sprintf("TimeToBoot = %s Running: Url = '%s'", time.Since(co.startTime).String(), url), map[string]interface{}{
			"url": url,
		})

		e = co.Provider.Run(fmt.Sprintf("%s:%d", cfg.Host, cfg.Port))
	}

	return e
}

func notImplemented(fname string) func() error {
	return func() error {
		panic(NewCoreError(fmt.Sprintf("Core.%s not implemented", fname)))

		return nil
	}
}

type logEntry struct {
	Request  string
	Response string
	Headers  map[string][]string
	Status   int
	Latency  time.Duration
	ClientIP string
	Method   string
	Path     string
}

type bodyLogWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w bodyLogWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w bodyLogWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

func CustomLogger(modulePath string, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		generatedUUID := uuid.New().String()
		c.Set("RequestID", generatedUUID)

		startTime := time.Now()

		buf, _ := io.ReadAll(c.Request.Body)
		c.Request.Body = io.NopCloser(bytes.NewBuffer(buf))

		blw := &bodyLogWriter{body: bytes.NewBufferString(""), ResponseWriter: c.Writer}
		c.Writer = blw

		c.Next()

		if c.Writer.Status() >= http.StatusBadRequest {
			// Read the request body
			body, _ := c.GetRawData()

			// Parse the body as a multipart form
			form, err := parseMultipartForm(body, c.Request.Header.Get("Content-Type"))
			if err != nil {
				// Failed to parse as a multipart form, log the raw body instead
				logWriter(generatedUUID, modulePath, "raw", body)
			} else {
				// Format and log the parsed form values
				content := "Content-Disposition: form-data;\n"
				for key, values := range form.Value {
					for _, value := range values {
						// Handle form-data request
						content += fmt.Sprintf("key=\"%s\"; value=\"%s\"\n", key, value)
					}
				}
				if content != "Content-Disposition: form-data;\n" {
					logWriter(generatedUUID, modulePath, "fields", []byte(content))
				}

				for key, files := range form.File {
					for _, file := range files {
						//Convert file to []byte
						arrayOfBytes, err := fileToArrayOfBytes(file)
						if err != nil {
							fmt.Printf("Error converting file to base64: %v\n", err)
						} else {
							extension := filepath.Ext(file.Filename)
							fileName := fmt.Sprintf("%s%s", key, extension)
							logWriter(generatedUUID, modulePath, fileName, arrayOfBytes)
						}
					}
				}
			}

			// Log the response and request
			entry := logEntry{
				Request:  string(buf),
				Response: fmt.Sprintf("%v", c.Writer.Status()),
				Headers:  c.Request.Header,
				Status:   c.Writer.Status(),
				Latency:  time.Since(startTime),
				ClientIP: c.ClientIP(),
				Method:   c.Request.Method,
				Path:     c.Request.URL.Path,
			}

			// Check if an error occurred during request processing
			if len(c.Errors) > 0 {
				// Get the line number where the error occurred
				_, file, line, _ := runtime.Caller(4)
				fmt.Printf("Error at line %d in file %s\n", line, file)
			}

			logger.WithFields(logrus.Fields{
				"id": generatedUUID,
				//"request":  entry.Request,
				//"response": entry.Response,
				//"headers":  entry.Headers,
				"status": entry.Status,
				//"latency":  entry.Latency,
				//"clientIP": entry.ClientIP,
				"method":   entry.Method,
				"path":     entry.Path,
				"response": blw.body.String(),
			}).Errorf("error with code %d", entry.Status)
		}
	}
}

// New return Core context, used as core of the application
func New(modulePath string, listener ...net.Listener) *Ctx {
	logger := logrus.New()
	logger.Formatter = &logrus.JSONFormatter{}

	file, err := os.OpenFile(modulePath+"/gin.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		logger.SetOutput(file)
	} else {
		logrus.Info("Failed to log to file, using default stderr")
	}

	// Load isDebug
	isDebug, _ = parser.String(os.Getenv("DEBUG")).ToBool()

	if !isDebug {
		gin.SetMode(gin.ReleaseMode)
	}

	p := HTTP()
	p.Engine.Use(CustomLogger(modulePath, logger))

	r := p.Router(DefaultCfg.Path)

	var lis net.Listener
	if len(listener) > 0 {
		lis = listener[0]
	}

	c := &Ctx{
		startTime: time.Now(),
		Provider:  p,
		Listener:  lis,
		Router:    r,
	}

	return c
}

// parseMultipartForm parses the raw request body as a multipart form
func parseMultipartForm(body []byte, contentType string) (*multipart.Form, error) {
	// Find the boundary from the content type header
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, err
	}
	boundary := params["boundary"]

	// Create a multipart reader with the given boundary
	reader := multipart.NewReader(strings.NewReader(string(body)), boundary)

	// Parse the multipart form
	return reader.ReadForm(0)
}

// fileToBase64 converts a file to base64 encoding
func fileToArrayOfBytes(file *multipart.FileHeader) ([]byte, error) {
	// Open the file
	openedFile, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer openedFile.Close()

	// Read the file contents
	fileContents, err := io.ReadAll(openedFile)
	if err != nil {
		return nil, err
	}

	return fileContents, nil
}

func logWriter(logId string, path string, filename string, content []byte) {
	dir := path + "/log_request/" + logId
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		fmt.Println("Error creating directory:", err)
		return
	}

	err = os.WriteFile(fmt.Sprintf("%s/log_request/%s/%s", path, logId, filename), content, 0644)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
}
