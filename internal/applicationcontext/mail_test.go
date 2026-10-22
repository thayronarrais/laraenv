package applicationcontext

import (
	"context"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func mailFixture(t *testing.T, handler http.HandlerFunc) *MailReader {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	r := NewMailReader()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(server.URL, "http://"))
		if err != nil {
			t.Logf("fixture connection: %v", err)
		}
		return conn, err
	}
	r.client.Transport = transport
	return r
}

func TestMailScopesListAndReadsRawWithoutMarkingRead(t *testing.T) {
	r := mailFixture(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Host != "127.0.0.1:8025" || req.Method != "GET" {
			t.Errorf("unsafe request %s %s", req.Method, req.URL)
		}
		switch req.URL.Path {
		case "/api/v1/search":
			if req.URL.Query().Get("query") != "tag:"+MailTag("demo") {
				t.Error("missing exact project query")
			}
			fmt.Fprintf(w, `{"messages_count":2,"messages":[{"ID":"message_demo1","Subject":"Welcome","Tags":[%q]},{"ID":"foreign_demo1","Subject":"PRIVATE_OTHER","Tags":["another-project"]}]}`, MailTag("demo"))
		case "/api/v1/message/message_demo1/raw":
			fmt.Fprint(w, "X-LaraEnv-Project: demo\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nWelcome token=secret\r\n")
		default:
			t.Errorf("unexpected endpoint (may mutate read status): %s", req.URL.Path)
			w.WriteHeader(404)
		}
	})
	got, err := r.List(context.Background(), "demo", 20)
	if err != nil || len(got.Messages) != 1 || got.Messages[0].ID != "message_demo1" {
		t.Fatalf("%+v %v", got, err)
	}
	body, err := r.Read(context.Background(), "demo", "message_demo1")
	if err != nil || body.Status != "available" || !strings.Contains(body.Text, "Welcome") || strings.Contains(body.Text, "secret") {
		t.Fatalf("%+v %v", body, err)
	}
}

func TestMailRejectsForeignRawAndUnsafeIDsAndBoundsResponses(t *testing.T) {
	calls := 0
	r := mailFixture(t, func(w http.ResponseWriter, req *http.Request) {
		calls++
		fmt.Fprint(w, "X-LaraEnv-Project: other\r\n\r\nPRIVATE_OTHER")
	})
	for _, id := range []string{"latest", "../etc", "https://example.com", "abc?x=1"} {
		if _, err := r.Read(context.Background(), "demo", id); err == nil {
			t.Errorf("accepted %q", id)
		}
	}
	if calls != 0 {
		t.Fatal("invalid IDs reached transport")
	}
	if _, err := r.Read(context.Background(), "demo", "foreign_demo1"); err == nil {
		t.Fatal("foreign mail disclosed")
	}
	r = mailFixture(t, func(w http.ResponseWriter, req *http.Request) { fmt.Fprint(w, strings.Repeat("x", (512<<10)+1)) })
	if _, err := r.List(context.Background(), "demo", 20); err == nil {
		t.Fatal("oversized response accepted")
	}
	r = mailFixture(t, func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "http://example.com/secret", 302)
	})
	if _, err := r.List(context.Background(), "demo", 20); err == nil {
		t.Fatal("redirect accepted")
	}
}

func TestMailDecodesTextAndOmitsAttachments(t *testing.T) {
	r := mailFixture(t, func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprint(w, "X-LaraEnv-Project: demo\r\nContent-Type: multipart/mixed; boundary=fixture\r\n\r\n--fixture\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=secret.txt\r\n\r\nPRIVATE_ATTACHMENT\r\n--fixture\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nOl=C3=A1 mundo\r\n--fixture--\r\n")
	})
	got, err := r.Read(context.Background(), "demo", "message_demo1")
	if err != nil || !strings.Contains(got.Text, "Olá mundo") || strings.Contains(got.Text, "PRIVATE") {
		t.Fatalf("%+v %v", got, err)
	}
	r = mailFixture(t, func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprint(w, "X-LaraEnv-Project: demo\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\nSGVsbG8gd29ybGQ=\r\n")
	})
	got, err = r.Read(context.Background(), "demo", "message_demo1")
	if err != nil || !strings.Contains(got.Text, "Hello world") {
		t.Fatalf("base64 body: %+v %v", got, err)
	}
}

func TestMailOmitsAttachmentsAtEveryMIMELevel(t *testing.T) {
	for _, disposition := range []string{
		"attachment", `inline; filename="private.txt"`, `inline; filename=""`,
		"inline; filename*=utf-8''private.txt", "inline; filename*=iso-8859-1''private.txt",
		"inline; filename*0*=utf-8''private; filename*1*=.txt",
	} {
		for _, nested := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/nested=%t", disposition, nested), func(t *testing.T) {
				part := "Content-Type: text/plain\r\nContent-Disposition: " + disposition + "\r\nContent-Transfer-Encoding: base64\r\n\r\nINVALID_PRIVATE_ATTACHMENT"
				message := part
				want := ""
				if nested {
					message = "Content-Type: multipart/mixed; boundary=outer\r\n\r\n--outer\r\nContent-Type: multipart/mixed; boundary=inner\r\n\r\n--inner\r\n" + part + "\r\n--inner--\r\n--outer\r\nContent-Type: text/plain\r\n\r\nVisible body\r\n--outer--\r\n"
					want = "Visible body"
				}
				r := mailFixture(t, func(w http.ResponseWriter, _ *http.Request) {
					fmt.Fprint(w, "X-LaraEnv-Project: demo\r\n"+message)
				})
				got, err := r.Read(context.Background(), "demo", "message_demo1")
				if err != nil || got.Status != "available" || strings.TrimSpace(got.Text) != want {
					t.Fatalf("attachment was decoded or returned: %+v, %v", got, err)
				}
			})
		}
	}
	for _, param := range []string{`name="private.txt"`, `filename="private.txt"`, "name*=iso-8859-1''private.txt"} {
		t.Run("content-type/"+param, func(t *testing.T) {
			r := mailFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, "X-LaraEnv-Project: demo\r\nContent-Type: text/plain; "+param+"\r\nContent-Transfer-Encoding: base64\r\n\r\nINVALID_PRIVATE_ATTACHMENT")
			})
			got, err := r.Read(context.Background(), "demo", "message_demo1")
			if err != nil || got.Status != "available" || got.Text != "" {
				t.Fatalf("named content was decoded or returned: %+v, %v", got, err)
			}
		})
	}
}

func TestMailDecodesExplicitCharset(t *testing.T) {
	for _, tc := range []struct{ charset, transfer, body, want string }{
		{"UTF-8", "quoted-printable", "Ol=C3=A1", "Olá"},
		{"ISO-8859-1", "quoted-printable", "Ol=E1", "Olá"},
		{"windows-1252", "quoted-printable", "Pre=E7o: =80 10", "Preço: € 10"},
	} {
		t.Run(tc.charset, func(t *testing.T) {
			r := mailFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(w, "X-LaraEnv-Project: demo\r\nContent-Type: text/plain; charset=%s\r\nContent-Transfer-Encoding: %s\r\n\r\n%s", tc.charset, tc.transfer, tc.body)
			})
			got, err := r.Read(context.Background(), "demo", "message_demo1")
			if err != nil || got.Status != "available" || got.Text != tc.want {
				t.Fatalf("charset was not decoded: %+v, %v", got, err)
			}
		})
	}
}

func TestMailUnknownCharsetIsUnavailable(t *testing.T) {
	r := mailFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "X-LaraEnv-Project: demo\r\nContent-Type: text/plain; charset=x-unknown\r\n\r\nUNDECODED_BODY")
	})
	got, err := r.Read(context.Background(), "demo", "message_demo1")
	if err == nil || !strings.Contains(err.Error(), "charset") || got.Status != "unavailable" || got.Text != "" {
		t.Fatalf("unknown charset was not explicitly withheld: %+v, %v", got, err)
	}
}

func TestMailOmitsHTMLBeforeDecodingUnknownCharset(t *testing.T) {
	r := mailFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "X-LaraEnv-Project: demo\r\nContent-Type: text/html; charset=x-unknown\r\nContent-Transfer-Encoding: base64\r\n\r\nINVALID_PRIVATE_HTML")
	})
	got, err := r.Read(context.Background(), "demo", "message_demo1")
	if err != nil || got.Status != "available" || got.Text != "" {
		t.Fatalf("HTML was decoded or returned: %+v, %v", got, err)
	}
}

func TestMailRejectsUnsupportedProjectMarkerCharset(t *testing.T) {
	r := mailFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "X-LaraEnv-Project: =?x-unknown?Q?caf=E9?=\r\nContent-Type: text/plain\r\n\r\nPRIVATE_FOREIGN_BODY")
	})
	got, err := r.Read(context.Background(), "café", "message_demo1")
	if err == nil || got.Status != "unavailable" || got.Text != "" {
		t.Fatalf("unsupported marker charset was accepted: %+v, %v", got, err)
	}
}

func TestMailBoundsDecodedCharsetExpansion(t *testing.T) {
	r := mailFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "X-LaraEnv-Project: demo\r\nContent-Type: text/plain; charset=windows-1252\r\n\r\n"+strings.Repeat("\x80", 20<<10))
	})
	got, err := r.Read(context.Background(), "demo", "message_demo1")
	want := strings.Repeat("€", (32<<10)/len("€"))
	if err != nil || got.Status != "available" || !got.Truncated || len(got.Text) > 32<<10 || !utf8.ValidString(got.Text) || got.Text != want {
		t.Fatalf("decoded size not bounded: status=%s, truncated=%t, bytes=%d, err=%v", got.Status, got.Truncated, len(got.Text), err)
	}
}

func TestMailDecodesProjectMarkerAndComparesExactly(t *testing.T) {
	for _, encoder := range []struct {
		name string
		enc  mime.WordEncoder
	}{{"quoted-printable", mime.QEncoding}, {"base64", mime.BEncoding}} {
		t.Run(encoder.name, func(t *testing.T) {
			project := "café"
			r := mailFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(w, "X-LaraEnv-Project: %s\r\nSubject: cafe\r\nTo: cafe@example.test\r\nContent-Type: text/plain\r\n\r\nOnly exact project", encoder.enc.Encode("utf-8", project))
			})
			got, err := r.Read(context.Background(), project, "message_demo1")
			if err != nil || got.Text != "Only exact project" {
				t.Fatalf("encoded exact marker rejected: %+v, %v", got, err)
			}
			for _, other := range []string{"cafe", "CAFÉ", "cafe\u0301"} {
				got, err := r.Read(context.Background(), other, "message_demo1")
				if err == nil || got.Text != "" || got.Status != "unavailable" {
					t.Fatalf("non-exact marker accepted for %q: %+v, %v", other, got, err)
				}
			}
		})
	}
}
