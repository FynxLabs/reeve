package drift

import "strings"

// errKind classifies a drift-check failure for retry purposes. Only network
// and auth-expiry failures are transient; everything else (engine crash,
// plan-parse error, policy failure) is permanent and never retried.
type errKind int

const (
	// errPermanent is any failure that retrying cannot fix.
	errPermanent errKind = iota
	// errTransientNetwork is a network failure reaching the engine or a cloud
	// SDK: retry up to retry_on_transient_error times.
	errTransientNetwork
	// errAuthExpired is an expired-credential failure: rebind (re-resolve
	// auth) and retry once.
	errAuthExpired
)

// networkSignatures are substrings (lower-cased) that identify a network
// failure reaching the engine or a cloud SDK. Conservative: only signatures
// that are unambiguously transport-level, plus the cloud throttle/5xx codes
// the AWS/GCP/Azure SDKs surface through the engine's stderr.
var networkSignatures = []string{
	"dial tcp",
	"connection refused",
	"connection reset",
	"connection timed out",
	"broken pipe",
	"no such host",
	"i/o timeout",
	"tls handshake timeout",
	"network is unreachable",
	"no route to host",
	"temporary failure in name resolution",
	"server misbehaving",
	"unexpected eof",
	"http2: server sent goaway",
	"requesterror",          // aws-sdk-go transport wrapper
	"requesttimeout",        // aws RequestTimeout / RequestTimeoutException
	"requestcanceled",       // aws context/transport cancel wrapper
	"throttling",            // aws Throttling / ThrottlingException
	"throttled",             // gcp/azure throttle wording
	"toomanyrequests",       // 429 TooManyRequestsException
	"rate exceeded",         // aws RequestLimitExceeded message
	"requestlimitexceeded",  // aws RequestLimitExceeded code
	"serviceunavailable",    // 503 ServiceUnavailable
	"service unavailable",   // 503 message form
	"internalservererror",   // 500 InternalServerError code
	"internal server error", // 500 message form
	"bad gateway",           // 502
	"gateway timeout",       // 504
	"could not connect",     // generic engine-side connect failure
	"error connecting",      // generic engine-side connect failure
	"transport is closing",  // grpc transport drop (pulumi plugins)
}

// authExpirySignatures identify an expired-credential failure. Kept narrow:
// only "expired" wording, never bare "unauthorized"/"forbidden"/"access
// denied" (those are usually permanent policy failures, not expiry).
var authExpirySignatures = []string{
	"expiredtoken", // aws ExpiredToken / ExpiredTokenException
	"the security token included in the request is expired",
	"token has expired",
	"token is expired",
	"token expired",
	"credentials have expired",
	"expired credentials",
	"credential has expired",
	"requestexpired", // aws RequestExpired
	"session token expired",
	"expired session token",
	"reauthenticate", // gcloud "please reauthenticate"
	"invalid_grant",  // OAuth2 expired/revoked refresh token
	"authenticationexpired",
}

// classifyDriftError maps a failure message onto an errKind. Matching is
// substring, case-insensitive, so it works on the raw engine/cloud-SDK
// stderr the adapters bubble up. Network signatures win over auth (a network
// blip that mentions auth is still a network blip); auth wins over permanent.
func classifyDriftError(msg string) errKind {
	if msg == "" {
		return errPermanent
	}
	low := strings.ToLower(msg)
	for _, s := range networkSignatures {
		if strings.Contains(low, s) {
			return errTransientNetwork
		}
	}
	for _, s := range authExpirySignatures {
		if strings.Contains(low, s) {
			return errAuthExpired
		}
	}
	return errPermanent
}
