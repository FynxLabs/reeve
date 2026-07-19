package gcs

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"google.golang.org/api/googleapi"
)

func TestIsPreconditionFailed(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "412 googleapi error",
			err: &googleapi.Error{
				Code:    http.StatusPreconditionFailed,
				Message: "At least one of the pre-conditions you specified did not hold.",
				Errors:  []googleapi.ErrorItem{{Reason: "conditionNotMet"}},
			},
			want: true,
		},
		{
			name: "409 generation conflict",
			err: &googleapi.Error{
				Code:    http.StatusConflict,
				Message: "The operation conflicted with another concurrent operation.",
			},
			want: true,
		},
		{
			name: "wrapped 412",
			err:  fmt.Errorf("write lock blob: %w", &googleapi.Error{Code: http.StatusPreconditionFailed}),
			want: true,
		},
		{
			name: "stringified conditionNotMet fallback",
			err:  errors.New("googleapi: Error 412: conditionNotMet"),
			want: true,
		},
		{
			name: "404 googleapi error",
			err:  &googleapi.Error{Code: http.StatusNotFound, Message: "object not found"},
			want: false,
		},
		{
			name: "unrelated error mentioning 412 bytes",
			err:  errors.New("read 412 bytes then connection reset"),
			want: false,
		},
		{
			name: "arbitrary error",
			err:  errors.New("boom"),
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPreconditionFailed(tc.err); got != tc.want {
				t.Fatalf("isPreconditionFailed(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
