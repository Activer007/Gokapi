//go:build test

package aws

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
)

type sec030AWSUnreadBody struct{ reads int }

func (reader *sec030AWSUnreadBody) Read([]byte) (int, error) {
	reader.reads++
	return 0, io.ErrUnexpectedEOF
}

func TestSEC030AWSContextRejectedBeforeRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	file := models.File{AwsBucket: "synthetic-bucket", SHA1: "synthetic-key"}
	for _, candidate := range []struct {
		name string
		ctx  context.Context
	}{{name: "canceled", ctx: ctx}, {name: "nil"}} {
		t.Run(candidate.name, func(t *testing.T) {
			reader := &sec030AWSUnreadBody{}
			if _, err := UploadContext(candidate.ctx, reader, file); err == nil || (candidate.ctx != nil && !errors.Is(err, context.Canceled)) {
				t.Error("invalid caller context did not stop upload")
			}
			if reader.reads != 0 {
				t.Error("rejected caller context consumed the upload body")
			}
			exists, size, err := FileExistsContext(candidate.ctx, file)
			if exists || size != 0 || err == nil || (candidate.ctx != nil && !errors.Is(err, context.Canceled)) {
				t.Error("invalid caller context was treated as a verified object lookup")
			}
		})
	}
	if !IsIncludedInBuild {
		reader := &sec030AWSUnreadBody{}
		if _, err := UploadContext(context.Background(), reader, file); err == nil || reader.reads != 0 {
			t.Error("unsupported AWS upload did not reject before reading")
		}
		if exists, size, err := FileExistsContext(context.Background(), file); exists || size != 0 || err == nil {
			t.Error("unsupported AWS lookup was treated as verified existence")
		}
	}
}
