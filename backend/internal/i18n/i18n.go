// Package i18n renders user-visible backend messages (change timeline entries,
// notification/email text and common API errors) in the language negotiated from
// the request's Accept-Language header. English is the fallback so that clients
// that send no preference — including the git push path — keep working unchanged.
package i18n

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// Supported language codes. Keep in sync with the frontend LANGUAGES list.
const (
	En = "en"
	Zh = "zh"
)

// catalog maps lang -> message key -> printf-style template. A key missing from
// a non-English catalog falls back to the English template.
var catalog = map[string]map[string]string{
	En: enMessages,
	Zh: zhMessages,
}

// T renders key in lang, applying args to the template via fmt.Sprintf when args
// are supplied. Unknown keys render as the key itself; unknown languages fall
// back to English.
func T(lang, key string, args ...any) string {
	tmpl, ok := catalog[lang][key]
	if !ok {
		tmpl, ok = enMessages[key]
		if !ok {
			return key
		}
	}
	if len(args) == 0 {
		return tmpl
	}
	return fmt.Sprintf(tmpl, args...)
}

// ParseAcceptLanguage picks the best supported language from an Accept-Language
// header value, defaulting to English. Only the primary subtag is considered
// (zh-CN, zh-Hans all collapse to "zh").
func ParseAcceptLanguage(header string) string {
	best, bestQ := En, -1.0
	for _, part := range strings.Split(header, ",") {
		tag, q := part, 1.0
		if i := strings.IndexByte(part, ';'); i >= 0 {
			tag = part[:i]
			for _, param := range strings.Split(part[i+1:], ";") {
				param = strings.TrimSpace(param)
				if strings.HasPrefix(param, "q=") {
					fmt.Sscanf(param[2:], "%f", &q)
				}
			}
		}
		primary := strings.ToLower(strings.TrimSpace(strings.SplitN(tag, "-", 2)[0]))
		switch primary {
		case Zh, "en":
			if q > bestQ {
				best, bestQ = primary, q
			}
		}
	}
	return best
}

type ctxKey int

const langKey ctxKey = 1

// WithLang returns a context carrying lang.
func WithLang(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, langKey, lang)
}

// LangFrom extracts the request language, defaulting to English.
func LangFrom(ctx context.Context) string {
	if lang, ok := ctx.Value(langKey).(string); ok && lang != "" {
		return lang
	}
	return En
}

// Middleware negotiates the request language and stores it in the context so
// downstream handlers can localize their messages.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang := ParseAcceptLanguage(r.Header.Get("Accept-Language"))
		next.ServeHTTP(w, r.WithContext(WithLang(r.Context(), lang)))
	})
}
