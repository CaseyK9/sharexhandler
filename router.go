package sharexhandler

import (
	"github.com/gorilla/mux"
	"net/http"
	"io"
	"mime"
	"mime/multipart"
	"bytes"
	"strings"
	"fmt"
)

// Path configuration:
// All paths have to start with a slash ("/")
type PathConfiguration struct {
	UploadPath string // Path where POST-Requests of ShareX are routing at. Example: /upload
	GetPath    string // Path where clients get their files. The Id in the path must be {id}. Example: /get/{id}
}

// This is the main class which is used to use the ShareX handler
type ShareXHandler struct {
	// The path configuration
	PathConfiguration *PathConfiguration
	// The Storage where files will be stored at/loaded from
	Storage Storage
	// A function which is called on every request (for example to set specific response headers).
	OutgoingFunction func(http.ResponseWriter, *http.Request)
	// Buffer size in bytes which is allocated when sending a file. Per default this is set to 1024.
	BufferSize int
	// The path has to start a slash ("/"). This is where the router gets bound on.
	Path string
	// This is used to respond to upload requests and refer the ShareX client to the right url. It has to end with a slash! Example: http://localhost:8080/
	ProtocolHost string
	// Whitelisted content types which will be displayed in the client`s browser.
	WhitelistedContentTypes []string
}

// This is the function which binds a ShareX handler router to the given path.
func (shareXHandler *ShareXHandler) BindToRouter(parentRouter *mux.Router) {
	router := parentRouter.PathPrefix(shareXHandler.Path).Subrouter()
	router.HandleFunc(shareXHandler.PathConfiguration.UploadPath, shareXHandler.handleUploadRequest)
	router.HandleFunc(shareXHandler.PathConfiguration.GetPath, shareXHandler.handleGetRequest)
}

// This method handles incoming POST upload request.
func (shareXHandler *ShareXHandler) handleUploadRequest(w http.ResponseWriter, req *http.Request) {
	if shareXHandler.OutgoingFunction != nil {
		shareXHandler.OutgoingFunction(w, req)
	}
	var err error
	_, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil {
		http.Error(w, "400 bad request", http.StatusBadRequest)
	} else {
		multipartReader := multipart.NewReader(req.Body, params["boundary"])
		entry := shareXHandler.Storage.NewStorageEntry()
		if err := entry.Save(); err != nil {
			http.Error(w, "500 an internal error occurred", http.StatusInternalServerError)
			panic(err)
		} else {
			id := entry.GetId()
			if writer, err := entry.GetWriter(); err != nil {
				http.Error(w, "500 an internal error occurred", http.StatusInternalServerError)
				panic(err)
			} else {
				defer writer.Close()
				var partErr error
				var part *multipart.Part
				part, partErr = multipartReader.NextPart()
				if partErr != nil {
					http.Error(w, "500 an internal error occurred", http.StatusInternalServerError)
					panic(partErr)
				} else {
					buf := new(bytes.Buffer)
					entry.SetContentType(part.Header.Get("Content-Type"))
					entry.SetFilename(part.FileName())
					for ; ; {
						if partErr == nil {
							buf.Reset()
							if _, err := io.Copy(buf, part); err != nil {
								http.Error(w, "500 an internal error occurred", http.StatusInternalServerError)
								panic(err)
							} else {
								writer.Write(buf.Bytes())
							}
						} else if partErr != io.EOF {
							http.Error(w, "500 an internal error occurred", http.StatusInternalServerError)
							panic(partErr)
						} else {
							break
						}
						part, partErr = multipartReader.NextPart()
					}
					buf.Reset()
					if err := entry.Update(); err != nil {
						http.Error(w, "500 an internal error occurred", http.StatusInternalServerError)
						panic(partErr)
					} else {
						w.WriteHeader(200)
						url := shareXHandler.ProtocolHost + id + entry.GetFilename()[strings.LastIndex(entry.GetFilename(), "."):]
						w.Write([]byte(url))
					}
				}
			}
		}
	}
}

var dispositionValueFormat = "%v; filename=\"%v\""

// This method handles get requests and shares files.
func (shareXHandler *ShareXHandler) handleGetRequest(w http.ResponseWriter, req *http.Request) {
	if shareXHandler.OutgoingFunction != nil {
		shareXHandler.OutgoingFunction(w, req)
	}
	vars := mux.Vars(req)
	id := vars["id"]
	id = id[:strings.LastIndex(id, ".")]
	if success, err, entry := shareXHandler.Storage.LoadStorageEntry(id); err != nil {
		http.Error(w, "500 an internal error occurred", http.StatusInternalServerError)
		panic(err)
	} else if !success {
		http.NotFound(w, req)
	} else if req.Header.Get("If-None-Match") == entry.GetETagValue() {
		w.WriteHeader(http.StatusNotModified)
	} else if reader, err := entry.GetReader(); err != nil {
		http.Error(w, "500 an internal error occurred", http.StatusInternalServerError)
		panic(err)
	} else {
		// content-disposition: inline; filename="javaw_2017-07-10_18-29-32.png"
		// content-disposition: attachment; filename="temp.html"
		for _, value := range shareXHandler.WhitelistedContentTypes {
			if strings.EqualFold(value, entry.GetContentType()) {
				w.Header().Set("Content-Disposition", fmt.Sprintf(dispositionValueFormat, "inline", entry.GetFilename()))
				goto inlinePassed
			}
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf(dispositionValueFormat, "attachment", entry.GetFilename()))
	inlinePassed:
		w.Header().Set("Content-Type", entry.GetContentType())
		w.Header().Set("ETag", entry.GetETagValue())
		w.WriteHeader(http.StatusOK)
		buf := make([]byte, shareXHandler.BufferSize)
		for {
			n, err := reader.Read(buf)
			if err != nil && err != io.EOF {
				http.Error(w, "500 an internal error occurred", http.StatusInternalServerError)
				panic(err)
			}
			if n == 0 {
				break
			}
			if _, err := w.Write(buf[:n]); err != nil {
				panic(err)
			}
		}
	}
}
