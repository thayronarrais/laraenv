package applicationcontext

import (
	"context"
	"testing"
)

func TestCaptureValidatesTraceAndFingerprintBeforeGrouping(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "storage/laraenv/context-2026-10-19.jsonl", `{"v":1,"project":"demo","traceId":"not/a/trace","time":"2026-10-19T11:00:00Z","kind":"query","data":{"operation":"select","fingerprint":"PRIVATE_INVALID"}}
{"v":1,"project":"demo","traceId":"1234567890abcdef1234567890abcdef","time":"2026-10-19T11:00:01Z","kind":"query","data":{"operation":"select","fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
{"v":1,"project":"demo","traceId":"1234567890abcdef1234567890abcdef","time":"2026-10-19T11:00:02Z","kind":"query","data":{"operation":"select","fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
`)
	got, err := ReadCapture(context.Background(), root, "demo", 20)
	if err != nil || len(got.Events) != 3 || got.Events[0].TraceID != "" || got.Events[0].Data["fingerprint"] != nil || len(got.Traces) != 1 || got.Traces[0].RepeatedQueries != 1 {
		t.Fatalf("%+v %v", got, err)
	}
}
