package s3

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// responseError builds the layered error the SDK produces for an HTTP
// failure: OperationError > ResponseError > (optional) APIError.
func responseError(status int, inner error) error {
	return &smithy.OperationError{
		ServiceID:     "S3",
		OperationName: "PutObject",
		Err: &awshttp.ResponseError{
			ResponseError: &smithyhttp.ResponseError{
				Response: &smithyhttp.Response{
					Response: &http.Response{StatusCode: status},
				},
				Err: inner,
			},
		},
	}
}

func TestIsPreconditionFailed(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "412 PreconditionFailed api error",
			err: &smithy.GenericAPIError{
				Code:    "PreconditionFailed",
				Message: "At least one of the pre-conditions you specified did not hold",
			},
			want: true,
		},
		{
			name: "409 ConditionalRequestConflict api error",
			err: &smithy.GenericAPIError{
				Code:    "ConditionalRequestConflict",
				Message: "A conflicting conditional operation is currently in progress against this resource.",
			},
			want: true,
		},
		{
			name: "412 wrapped in operation + response error",
			err: responseError(http.StatusPreconditionFailed, &smithy.GenericAPIError{
				Code: "PreconditionFailed",
			}),
			want: true,
		},
		{
			name: "409 wrapped in operation + response error",
			err: responseError(http.StatusConflict, &smithy.GenericAPIError{
				Code: "ConditionalRequestConflict",
			}),
			want: true,
		},
		{
			name: "bare 412 response error without api error code",
			err:  responseError(http.StatusPreconditionFailed, errors.New("precondition failed")),
			want: true,
		},
		{
			name: "fmt-wrapped api error",
			err: fmt.Errorf("put lock blob: %w", &smithy.GenericAPIError{
				Code: "ConditionalRequestConflict",
			}),
			want: true,
		},
		{
			name: "unrelated 409 OperationAborted is not a CAS conflict",
			err:  responseError(http.StatusConflict, &smithy.GenericAPIError{Code: "OperationAborted"}),
			want: false,
		},
		{
			name: "no such key",
			err:  &s3types.NoSuchKey{},
			want: false,
		},
		{
			name: "arbitrary error",
			err:  errors.New("connection reset by peer"),
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

func TestIsNotFound(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"NoSuchKey typed", &s3types.NoSuchKey{}, true},
		{"NoSuchBucket typed", &s3types.NoSuchBucket{}, true},
		{"NotFound api error", &smithy.GenericAPIError{Code: "NotFound"}, true},
		{"wrapped NoSuchKey", fmt.Errorf("get: %w", &s3types.NoSuchKey{}), true},
		{"precondition failure is not not-found", &smithy.GenericAPIError{Code: "PreconditionFailed"}, false},
		{"arbitrary error", errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNotFound(tc.err); got != tc.want {
				t.Fatalf("isNotFound(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
