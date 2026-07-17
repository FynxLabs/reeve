package config

import (
	"os"
	"reflect"
	"strings"
)

// expandEnvRef unwraps an exact "${env:NAME}" reference to os.Getenv(NAME);
// any other string is returned unchanged. Mirrors the notify helper's
// behavior, now applied config-wide.
func expandEnvRef(s string) string {
	if strings.HasPrefix(s, "${env:") && strings.HasSuffix(s, "}") {
		return os.Getenv(s[len("${env:") : len(s)-1])
	}
	return s
}

// ExpandEnv rewrites every "${env:NAME}" string field in the config to the
// value of the named environment variable. Applied after load so any field -
// bucket, auth provider creds, otel endpoint/headers, drift sink credentials -
// can reference an env var, not just slack.auth_token. Non-matching strings
// are left untouched.
func (c *Config) ExpandEnv() {
	if c == nil {
		return
	}
	expandEnvRefs(reflect.ValueOf(c))
}

func expandEnvRefs(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			expandEnvRefs(v.Elem())
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).PkgPath != "" {
				continue // unexported: not settable, skip
			}
			expandEnvRefs(v.Field(i))
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			expandEnvRefs(v.Index(i))
		}
	case reflect.Map:
		// Map values aren't addressable; copy, expand, reinsert.
		for _, k := range v.MapKeys() {
			mv := v.MapIndex(k)
			cp := reflect.New(mv.Type()).Elem()
			cp.Set(mv)
			expandEnvRefs(cp)
			v.SetMapIndex(k, cp)
		}
	case reflect.String:
		if v.CanSet() {
			v.SetString(expandEnvRef(v.String()))
		}
	}
}
