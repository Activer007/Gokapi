package uploadreceive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"testing"
)

func assertReleased(t *testing.T, p *Pool) {
	t.Helper()
	if p.active != 0 || p.reserved != 0 || len(p.leases) != 0 || len(p.orphans) != 0 {
		t.Fatalf("unreleased receiver: active=%d reserved=%d leases=%d orphans=%d", p.active, p.reserved, len(p.leases), len(p.orphans))
	}
}

func TestReceivePayloadAndFieldLimits(t *testing.T) {
	fields33 := make([][2]string, 33)
	for i := range fields33 {
		fields33[i] = [2]string{fmt.Sprintf("field%d", i), "x"}
	}
	for _, tc := range []struct {
		name    string
		fields  [][2]string
		payload []byte
		want    error
	}{
		{"exact-file", nil, []byte("1234"), nil},
		{"oversize-file", nil, []byte("12345"), ErrTooLarge},
		{"duplicate-field", [][2]string{{"expiry", "1"}, {"expiry", "2"}}, nil, ErrInvalid},
		{"field-count", fields33, nil, ErrTooLarge},
		{"exact-fields", [][2]string{{"a", strings.Repeat("v", maxFieldBytes-1)}}, nil, nil},
		{"field-bytes", [][2]string{{"a", strings.Repeat("v", maxFieldBytes)}}, nil, ErrTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New(1, 32<<20)
			if err != nil {
				t.Fatal(err)
			}
			u, err := p.Receive(httptest.NewRecorder(), receiveRequest(t, true, tc.fields, tc.payload, true), t.TempDir(), 4, 0)
			if tc.want == nil {
				if err != nil {
					t.Fatal(err)
				}
				if err := u.Close(); err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			assertReleased(t, p)
		})
	}
}

func TestReceiveCountsActualBodyAndEpilogue(t *testing.T) {
	t.Run("declared-length-is-not-a-cap", func(t *testing.T) {
		p, _ := New(1, 16<<20)
		r := receiveRequest(t, true, nil, bytes.Repeat([]byte("x"), 1<<20), false)
		r.ContentLength = 1
		u, err := p.Receive(httptest.NewRecorder(), r, t.TempDir(), 2<<20, 0)
		if err != nil {
			t.Fatal(err)
		}
		if p.reserved <= baseCharge+4 || u.Header.Size != 1<<20 {
			t.Fatal("actual body did not grow reservation")
		}
		if err := u.Close(); err != nil {
			t.Fatal(err)
		}
		assertReleased(t, p)
	})
	t.Run("unknown-growth-rejects-before-writing-unreserved-body", func(t *testing.T) {
		p, _ := New(1, baseCharge)
		_, err := p.Receive(httptest.NewRecorder(), receiveRequest(t, true, nil, []byte("x"), true), t.TempDir(), 1, 0)
		if !errors.Is(err, ErrCapacity) {
			t.Fatalf("growth = %v", err)
		}
		assertReleased(t, p)
	})
	t.Run("raw-epilogue-counted", func(t *testing.T) {
		p, _ := New(1, 16<<20)
		r := receiveRequest(t, true, nil, nil, true)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), strings.NewReader(strings.Repeat("x", int(envelopeBytes)))))
		_, err = p.Receive(httptest.NewRecorder(), r, t.TempDir(), 0, 0)
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("epilogue = %v", err)
		}
		assertReleased(t, p)
	})
	t.Run("small-file-cannot-use-file-limit-for-epilogue", func(t *testing.T) {
		p, _ := New(1, 32<<20)
		r := receiveRequest(t, true, nil, []byte("x"), true)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), strings.NewReader(strings.Repeat("x", int(envelopeBytes)))))
		_, err = p.Receive(httptest.NewRecorder(), r, t.TempDir(), 4<<20, 0)
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("non-file overhead = %v", err)
		}
		assertReleased(t, p)
	})
}

func TestReceiveRejectsInvalidFilePartsAndPreservesTransferSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, partName string
		count          int
		want           error
		encoding       string
		wantBytes      string
	}{
		{"missing-file", "file", 0, ErrInvalid, "quoted-printable", ""},
		{"wrong-part", "another", 1, ErrInvalid, "quoted-printable", ""},
		{"two-files", "file", 2, ErrInvalid, "quoted-printable", ""},
		{"legacy-quoted-printable", "file", 1, nil, "QuOtEd-PrInTaBlE", "a=b"},
		{"base64-label-is-raw", "file", 1, nil, "base64", "a=3Db"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			writer := multipart.NewWriter(&b)
			for i := 0; i < tc.count; i++ {
				h := make(textproto.MIMEHeader)
				h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename="payload"`, tc.partName))
				h.Set("Content-Transfer-Encoding", tc.encoding)
				part, err := writer.CreatePart(h)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := io.WriteString(part, "a=3Db"); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/upload", &b)
			r.Header.Set("Content-Type", writer.FormDataContentType())
			p, _ := New(1, 16<<20)
			u, err := p.Receive(httptest.NewRecorder(), r, t.TempDir(), 100, 0)
			if tc.want != nil {
				if !errors.Is(err, tc.want) {
					t.Fatalf("got %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(u.File)
				if err != nil || string(data) != tc.wantBytes || u.Header.Size != int64(len(tc.wantBytes)) {
					t.Fatalf("wrong decoded payload/size: %q size=%d %v", data, u.Header.Size, err)
				}
				wantEncoding := tc.encoding
				if strings.EqualFold(wantEncoding, "quoted-printable") {
					wantEncoding = ""
				}
				if got := u.Header.Header.Get("Content-Transfer-Encoding"); got != wantEncoding {
					t.Fatalf("transfer header = %q, want %q", got, wantEncoding)
				}
				if err := u.Close(); err != nil {
					t.Fatal(err)
				}
			}
			assertReleased(t, p)
		})
	}
}

func quotedPrintableRequest(t *testing.T, first, unknown bool, payload []byte, field string) (*http.Request, int64) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writePart := func(disposition string, data []byte) int64 {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", disposition)
		h.Set("Content-Transfer-Encoding", "QuOtEd-PrInTaBlE")
		part, err := writer.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		start := body.Len()
		encoder := quotedprintable.NewWriter(part)
		if _, err := encoder.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := encoder.Close(); err != nil {
			t.Fatal(err)
		}
		return int64(body.Len() - start)
	}
	writeField := func() {
		writePart(`form-data; name="comment"`, []byte(field))
	}
	if !first {
		writeField()
	}
	wireFileBytes := writePart(`form-data; name="file"; filename="encoded.bin"`, payload)
	if first {
		writeField()
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/upload", bytes.NewReader(body.Bytes()))
	r.Header.Set("Content-Type", writer.FormDataContentType())
	if unknown {
		r.ContentLength = -1
	}
	return r, wireFileBytes
}

func TestReceiveQuotedPrintableWireAccountingAndDecodedSize(t *testing.T) {
	// This real encoding expands file bytes by more than the entire MIME
	// envelope allowance. It must be charged as file wire bytes, rather than
	// falsely rejected as headers/fields after decoding.
	payload := bytes.Repeat([]byte{0, 0xff, '=', 1, 2}, 128<<10)
	for _, first := range []bool{true, false} {
		for _, unknown := range []bool{true, false} {
			t.Run(fmt.Sprintf("file-first=%t/unknown=%t", first, unknown), func(t *testing.T) {
				r, wireFileBytes := quotedPrintableRequest(t, first, unknown, payload, "field=\x00")
				if wireFileBytes-int64(len(payload)) <= envelopeBytes {
					t.Fatal("fixture does not distinguish raw file bytes from decoded size")
				}
				p, err := New(1, 64<<20)
				if err != nil {
					t.Fatal(err)
				}
				u, err := p.Receive(httptest.NewRecorder(), r, t.TempDir(), 4<<20, 0)
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(u.File)
				if err != nil || !bytes.Equal(data, payload) || u.Header.Size != int64(len(payload)) {
					t.Fatalf("decoded payload mismatch: bytes=%d size=%d %v", len(data), u.Header.Size, err)
				}
				if u.Header.Header.Get("Content-Transfer-Encoding") != "" || u.Fields.Get("comment") != "field=\x00" {
					t.Fatal("legacy transfer header/field semantics changed")
				}
				if err := u.Close(); err != nil {
					t.Fatal(err)
				}
				assertReleased(t, p)
			})
		}
	}
}

func TestReceiveQuotedPrintableDecodedLimits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload []byte
		field   string
		want    error
	}{
		{"exact-file", []byte{0, '=', 1, 2}, "ok", nil},
		{"oversize-decoded-file", []byte{0, '=', 1, 2, 3}, "ok", ErrTooLarge},
		{"exact-decoded-fields", nil, strings.Repeat("=", maxFieldBytes-len("comment")), nil},
		{"oversize-decoded-fields", nil, strings.Repeat("=", maxFieldBytes-len("comment")+1), ErrTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := quotedPrintableRequest(t, true, true, tc.payload, tc.field)
			p, err := New(1, 32<<20)
			if err != nil {
				t.Fatal(err)
			}
			u, err := p.Receive(httptest.NewRecorder(), r, t.TempDir(), 4, 0)
			if tc.want != nil {
				if !errors.Is(err, tc.want) {
					t.Fatalf("decoded limit = %v, want %v", err, tc.want)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if u.Header.Size != int64(len(tc.payload)) || u.Fields.Get("comment") != tc.field {
					t.Fatal("decoded limits changed accepted payload/fields")
				}
				if err := u.Close(); err != nil {
					t.Fatal(err)
				}
			}
			assertReleased(t, p)
		})
	}
}

func TestReceiveCapacitySpaceAndArithmetic(t *testing.T) {
	for _, slots := range []int64{0, -1, 65} {
		if _, err := New(slots, 1); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid slots", slots, err)
		}
	}
	for _, budget := range []int64{0, -1} {
		if _, err := New(1, budget); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid budget", budget, err)
		}
	}
	for _, body := range []int64{1 << 30, 2 << 30} {
		got, err := reservationCharge(body)
		if err != nil || got != 4*body+baseCharge {
			t.Fatalf("GiB calculation: %d %v", got, err)
		}
	}
	if _, err := reservationCharge(math.MaxInt64); !errors.Is(err, ErrCapacity) {
		t.Fatal("overflow accepted", err)
	}
	p, _ := New(2, 16<<20)
	p.free = func(string) (uint64, error) { return 100, nil }
	_, err := p.Receive(httptest.NewRecorder(), receiveRequest(t, true, nil, nil, true), t.TempDir(), 0, 100)
	if !errors.Is(err, ErrSpace) {
		t.Fatal("floor ignored", err)
	}
	assertReleased(t, p)
	p.free = func(string) (uint64, error) { return 0, os.ErrPermission }
	_, err = p.Receive(httptest.NewRecorder(), receiveRequest(t, true, nil, nil, true), p.dataDir, 0, 0)
	if !errors.Is(err, ErrSpace) {
		t.Fatal("free-space error ignored", err)
	}
	assertReleased(t, p)
}

type canceledAfterRead struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (r canceledAfterRead) Read(b []byte) (int, error) {
	n, e := r.reader.Read(b)
	r.cancel()
	return n, e
}

func TestReceiveCancellationAndReadFailureCleanup(t *testing.T) {
	for _, during := range []bool{false, true} {
		p, _ := New(1, 16<<20)
		r := receiveRequest(t, true, nil, bytes.Repeat([]byte("x"), 1<<16), true)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		r = r.WithContext(ctx)
		if during {
			r.Body = io.NopCloser(canceledAfterRead{r.Body, cancel})
		} else {
			cancel()
		}
		_, err := p.Receive(httptest.NewRecorder(), r, t.TempDir(), 1<<20, 0)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation = %v", err)
		}
		assertReleased(t, p)
	}
	p, _ := New(1, 16<<20)
	r := receiveRequest(t, true, nil, []byte("x"), true)
	r.Body = io.NopCloser(io.MultiReader(strings.NewReader("--broken\r\n"), errorReader{}))
	_, err := p.Receive(httptest.NewRecorder(), r, t.TempDir(), 1<<20, 0)
	if err == nil {
		t.Fatal("reader failure accepted")
	}
	assertReleased(t, p)
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// Compile-time assertion: the receiver accepts the standard HTTP request API.
var _ func(http.ResponseWriter, *http.Request, string, int64, int64) (*Upload, error) = (&Pool{}).Receive
