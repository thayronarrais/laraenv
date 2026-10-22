package applicationcontext

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
)

const mailResponseLimit = 512 << 10
const mailTextLimit = 32 << 10
const mailEndpoint = "http://127.0.0.1:8025"

var mailID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,80}$`)

// ParseMediaType may discard RFC 2231 names with unsupported charsets. Their
// presence still identifies named content that must not be returned as a body.
var mailFilename = regexp.MustCompile(`(?i)(?:^|;)\s*(?:filename|name)(?:\*[0-9]+\*?|\*)?\s*=`)

type MailReader struct{ client *http.Client }
type MailAddress struct {
	Name    string `json:"Name,omitempty"`
	Address string `json:"Address,omitempty"`
}
type MailSummary struct {
	ID      string        `json:"ID"`
	Subject string        `json:"Subject"`
	From    MailAddress   `json:"From"`
	To      []MailAddress `json:"To"`
	Created string        `json:"Created"`
	Tags    []string      `json:"Tags"`
}
type MailList struct {
	Status     string        `json:"status"`
	Message    string        `json:"message,omitempty"`
	ProjectTag string        `json:"projectTag"`
	Messages   []MailSummary `json:"messages"`
	Truncated  bool          `json:"truncated"`
}
type MailMessage struct {
	Status    string `json:"status"`
	ID        string `json:"id"`
	Text      string `json:"text"`
	Message   string `json:"message,omitempty"`
	Truncated bool   `json:"truncated"`
}

func MailTag(project string) string {
	sum := sha256.Sum256([]byte(project))
	return "laraenv-" + hex.EncodeToString(sum[:8])
}
func NewMailReader() *MailReader {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = (&net.Dialer{Timeout: 2 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = 3 * time.Second
	return &MailReader{client: &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (r *MailReader) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mailEndpoint+path, nil)
	if err != nil {
		return nil, errors.New("invalid Mailpit request")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, errors.New("local Mailpit unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("local Mailpit request failed; authentication/custom ports are not supported")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, mailResponseLimit+1))
	if err != nil || len(data) > mailResponseLimit {
		return nil, errors.New("Mailpit response exceeds safe read limit")
	}
	return data, nil
}
func (r *MailReader) List(ctx context.Context, project string, count int) (MailList, error) {
	out := MailList{Status: "unavailable", ProjectTag: MailTag(project), Messages: []MailSummary{}}
	if count < 0 || count > 50 {
		return out, errors.New("mail limit must be between 1 and 50")
	}
	if count == 0 {
		count = 20
	}
	query := url.Values{"query": {"tag:" + out.ProjectTag}, "limit": {strconv.Itoa(count)}, "start": {"0"}}
	data, err := r.get(ctx, "/api/v1/search?"+query.Encode())
	if err != nil {
		return out, err
	}
	var page struct {
		Messages []MailSummary `json:"messages"`
		Count    int           `json:"messages_count"`
	}
	if json.Unmarshal(data, &page) != nil || page.Messages == nil {
		return out, errors.New("unsupported Mailpit response")
	}
	for _, item := range page.Messages {
		belongs := false
		for _, tag := range item.Tags {
			if strings.EqualFold(tag, out.ProjectTag) {
				belongs = true
			}
		}
		if !belongs || !mailID.MatchString(item.ID) {
			continue
		}
		item.Subject = boundedText(Redact(item.Subject), 1024)
		item.From.Name = boundedText(item.From.Name, 256)
		item.From.Address = boundedText(item.From.Address, 256)
		if len(item.To) > 20 {
			item.To = item.To[:20]
			out.Truncated = true
		}
		for i := range item.To {
			item.To[i].Name = boundedText(item.To[i].Name, 256)
			item.To[i].Address = boundedText(item.To[i].Address, 256)
		}
		item.Tags = []string{out.ProjectTag}
		item.Created = boundedText(item.Created, 64)
		out.Messages = append(out.Messages, item)
		if len(out.Messages) == count {
			break
		}
	}
	out.Truncated = out.Truncated || page.Count > len(out.Messages)
	if len(out.Messages) == 0 {
		out.Message = "No explicitly project-tagged messages; untagged mail is not attributed. Enable the integration mail marker or add X-Tags: " + out.ProjectTag
		return out, nil
	}
	out.Status = "available"
	return out, nil
}
func (r *MailReader) Read(ctx context.Context, project, id string) (MailMessage, error) {
	out := MailMessage{Status: "unavailable", ID: id}
	if !mailID.MatchString(id) || id == "latest" {
		return out, errors.New("use an exact message ID from project mail_list")
	}
	// The summary endpoint marks a message as read. DownloadRaw does not, so
	// diagnostics can inspect plain text without changing mailbox state.
	data, err := r.get(ctx, "/api/v1/message/"+id+"/raw")
	if err != nil {
		return out, err
	}
	message, err := mail.ReadMessage(strings.NewReader(string(data)))
	if err != nil {
		return out, errors.New("invalid Mailpit message")
	}
	marker, err := (&mime.WordDecoder{CharsetReader: mailCharsetReader}).DecodeHeader(message.Header.Get("X-LaraEnv-Project"))
	if err != nil || marker != project {
		return out, errors.New("message lacks the exact project marker; content withheld")
	}
	text, err := mailPlainText(message.Header.Get("Content-Type"), message.Header.Get("Content-Disposition"), message.Header.Get("Content-Transfer-Encoding"), message.Body, 0)
	if err != nil {
		return out, err
	}
	out.Truncated = len(text) > mailTextLimit
	out.Text = strings.ToValidUTF8(Redact(text), "�")
	if len(out.Text) > mailTextLimit {
		out.Truncated = true
		out.Text = strings.ToValidUTF8(out.Text[:mailTextLimit], "")
	}
	out.Status = "available"
	if text == "" {
		out.Message = "No plain-text body; HTML and attachments are omitted"
	}
	return out, nil
}
func mailPlainText(contentType, disposition, transferEncoding string, body io.Reader, depth int) (string, error) {
	if depth > 4 {
		return "", errors.New("mail MIME nesting limit exceeded")
	}
	if mailFilename.MatchString(contentType) || mailFilename.MatchString(disposition) {
		return "", nil
	}
	if disposition != "" {
		kind, _, err := mime.ParseMediaType(disposition)
		if err != nil {
			return "", errors.New("invalid message content disposition")
		}
		if kind == "attachment" {
			return "", nil
		}
	}
	if contentType == "" {
		contentType = "text/plain"
	}
	kind, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", errors.New("invalid message content type")
	}
	if strings.HasPrefix(kind, "multipart/") {
		reader := multipart.NewReader(mailTransferReader(transferEncoding, body), params["boundary"])
		for i := 0; i < 20; i++ {
			// Advancing scans past skipped raw parts without transfer decoding them.
			part, err := reader.NextRawPart()
			if err == io.EOF {
				return "", nil
			}
			if err != nil {
				return "", errors.New("invalid multipart mail")
			}
			text, err := mailPlainText(part.Header.Get("Content-Type"), part.Header.Get("Content-Disposition"), part.Header.Get("Content-Transfer-Encoding"), part, depth+1)
			if err != nil {
				return "", err
			}
			if text != "" {
				return text, nil
			}
		}
		return "", errors.New("mail MIME part limit exceeded")
	}
	if kind != "text/plain" {
		return "", nil
	}
	decoded, err := mailCharsetReader(params["charset"], mailTransferReader(transferEncoding, body))
	if err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(decoded, mailTextLimit+1))
	if err != nil {
		return "", errors.New("mail text unavailable")
	}
	return string(data), nil
}

func mailCharsetReader(charset string, body io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		return body, nil
	case "iso-8859-1", "iso8859-1", "iso_8859-1", "latin1", "latin-1":
		return charmap.ISO8859_1.NewDecoder().Reader(body), nil
	case "windows-1252", "windows1252", "cp1252":
		return charmap.Windows1252.NewDecoder().Reader(body), nil
	default:
		return nil, errors.New("unsupported mail charset; content withheld")
	}
}

func mailTransferReader(encoding string, reader io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, reader)
	case "quoted-printable":
		return quotedprintable.NewReader(reader)
	default:
		return reader
	}
}
