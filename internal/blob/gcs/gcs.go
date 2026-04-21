// Package gcs is the Google Cloud Storage blob adapter. Atomic writes
// via generation preconditions (If-Generation-Match). Uses application
// default credentials from the environment.
package gcs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"

	"github.com/thefynx/reeve/internal/blob"
)

// Store implements blob.Store against a GCS bucket.
type Store struct {
	client *storage.Client
	bucket string
	prefix string
}

// Options configures New.
type Options struct {
	Bucket string
	Prefix string
}

// New returns a Store. Uses ADC from the environment.
func New(ctx context.Context, opts Options) (*Store, error) {
	c, err := storage.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	prefix := opts.Prefix
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return &Store{client: c, bucket: opts.Bucket, prefix: prefix}, nil
}

func (s *Store) fullKey(k string) string { return s.prefix + k }

// Get reads an object.
func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, *blob.Metadata, error) {
	obj := s.client.Bucket(s.bucket).Object(s.fullKey(key))
	attrs, err := obj.Attrs(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, nil, blob.ErrNotFound
		}
		return nil, nil, err
	}
	r, err := obj.NewReader(ctx)
	if err != nil {
		return nil, nil, err
	}
	md := &blob.Metadata{
		ETag:         strconv.FormatInt(attrs.Generation, 10),
		LastModified: attrs.Updated.Unix(),
		Size:         attrs.Size,
	}
	return r, md, nil
}

// Put writes unconditionally.
func (s *Store) Put(ctx context.Context, key string, r io.Reader) (*blob.Metadata, error) {
	obj := s.client.Bucket(s.bucket).Object(s.fullKey(key))
	w := obj.NewWriter(ctx)
	n, err := io.Copy(w, r)
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return &blob.Metadata{
		ETag: strconv.FormatInt(w.Attrs().Generation, 10),
		Size: n,
	}, nil
}

// PutIfMatch uses generation preconditions. Empty ifMatch → DoesNotExist.
func (s *Store) PutIfMatch(ctx context.Context, key string, r io.Reader, ifMatch string) (*blob.Metadata, error) {
	obj := s.client.Bucket(s.bucket).Object(s.fullKey(key))
	if ifMatch == "" {
		obj = obj.If(storage.Conditions{DoesNotExist: true})
	} else {
		gen, err := strconv.ParseInt(ifMatch, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("gcs: ifMatch must be a generation number: %w", err)
		}
		obj = obj.If(storage.Conditions{GenerationMatch: gen})
	}
	w := obj.NewWriter(ctx)
	n, err := io.Copy(w, r)
	if err != nil {
		_ = w.Close()
		if isPreconditionFailed(err) {
			return nil, blob.ErrPreconditionFailed
		}
		return nil, err
	}
	if err := w.Close(); err != nil {
		if isPreconditionFailed(err) {
			return nil, blob.ErrPreconditionFailed
		}
		return nil, err
	}
	return &blob.Metadata{
		ETag: strconv.FormatInt(w.Attrs().Generation, 10),
		Size: n,
	}, nil
}

// Delete removes an object. Missing is silent.
func (s *Store) Delete(ctx context.Context, key string) error {
	err := s.client.Bucket(s.bucket).Object(s.fullKey(key)).Delete(ctx)
	if err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
		return err
	}
	return nil
}

// List returns keys under the prefix.
func (s *Store) List(ctx context.Context, prefix string) ([]string, error) {
	it := s.client.Bucket(s.bucket).Objects(ctx, &storage.Query{Prefix: s.fullKey(prefix)})
	var out []string
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		out = append(out, strings.TrimPrefix(attrs.Name, s.prefix))
	}
	return out, nil
}

func isPreconditionFailed(err error) bool {
	var ge *googleAPIError
	if errors.As(err, &ge) {
		return ge.Code == 412
	}
	s := err.Error()
	return strings.Contains(s, "conditionNotMet") || strings.Contains(s, "Precondition Failed") || strings.Contains(s, "412")
}

// googleAPIError is the subset we need from googleapi.Error.
type googleAPIError struct{ Code int }

func (e *googleAPIError) Error() string { return fmt.Sprintf("googleapi error %d", e.Code) }

// compile-time check
var _ blob.Store = (*Store)(nil)
