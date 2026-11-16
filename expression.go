package warp

import (
	"fmt"
	"slices"
	"strings"
)

// ExpressionPattern is the regular expression pattern matching a WARP config
// expression of the form ${<scheme>:<target>}.
const ExpressionPattern = `^\$\{[a-z][a-z0-9+.-]*:[^}\r\n]+\}$`

// Expression represents a parsed declarative value expression of the form
// ${<scheme>:<target>} (for example, "${env:OPENAI_API_KEY}" or
// "${file:~/.secrets/idp-secret}").
type Expression struct {
	Raw    string
	Scheme string
	Target string
}

// ExpressionValidator validates the target portion of an expression for a
// specific scheme (for example, checking that an "env" target is a valid
// environment variable name).
type ExpressionValidator func(target string) error

// ValidateOptions configures Registry and ProviderAuth validation, including
// custom expression scheme validators and scheme allowlists.
type ValidateOptions struct {
	// ExpressionValidators registers custom validators for expression schemes
	// (or overrides the built-in validators for "env" and "file").
	ExpressionValidators map[string]ExpressionValidator

	// AllowedExpressionSchemes, when non-empty, restricts expressions to only
	// the listed schemes. When empty, any syntactically valid scheme is allowed.
	AllowedExpressionSchemes []string
}

// ParseExpression parses s if it is a single, complete expression of the form
// ${<scheme>:<target>}. Leading or trailing whitespace or surrounding text
// causes ParseExpression to return false.
func ParseExpression(s string) (Expression, bool) {
	if len(s) < 5 || !strings.HasPrefix(s, "${") || !strings.HasSuffix(s, "}") {
		return Expression{}, false
	}
	inner := s[2 : len(s)-1]
	if strings.ContainsAny(inner, "{}\r\n") {
		return Expression{}, false
	}
	scheme, target, found := strings.Cut(inner, ":")
	if !found || !isValidExpressionScheme(scheme) || strings.TrimSpace(target) == "" {
		return Expression{}, false
	}
	return Expression{
		Raw:    s,
		Scheme: scheme,
		Target: target,
	}, true
}

// IsExpression reports whether s is a single, valid ${<scheme>:<target>} expression.
func IsExpression(s string) bool {
	_, ok := ParseExpression(s)
	return ok
}

// ExtractExpressions scans s for ${...} expressions and parses each one.
// Plain text without "${" returns nil, nil. An unclosed or malformed "${...}"
// token returns an error.
func ExtractExpressions(s string) ([]Expression, error) {
	if !strings.Contains(s, "${") {
		return nil, nil
	}
	var exprs []Expression
	rest := s
	for {
		idx := strings.Index(rest, "${")
		if idx < 0 {
			break
		}
		rest = rest[idx:]
		end := strings.IndexByte(rest, '}')
		if end < 0 {
			return nil, fmt.Errorf("unclosed expression in %q", s)
		}
		raw := rest[:end+1]
		expr, ok := ParseExpression(raw)
		if !ok {
			return nil, fmt.Errorf("malformed expression %q (expected ${<scheme>:<ref>})", raw)
		}
		exprs = append(exprs, expr)
		rest = rest[end+1:]
	}
	return exprs, nil
}

// ValidateExpression validates that s is a single valid ${<scheme>:<target>}
// expression (rejecting plaintext literals) and satisfies any built-in or
// configured scheme constraints.
func ValidateExpression(s string, opts *ValidateOptions) error {
	expr, ok := ParseExpression(s)
	if !ok {
		return fmt.Errorf("must be a ${<scheme>:<ref>} expression, not a plaintext literal")
	}
	return validateExpression(expr, opts)
}

// ValidateStringWithExpressions allows plain literal strings as well as strings
// containing one or more ${<scheme>:<target>} expressions, validating every
// embedded expression.
func ValidateStringWithExpressions(s string, opts *ValidateOptions) error {
	exprs, err := ExtractExpressions(s)
	if err != nil {
		return err
	}
	for _, expr := range exprs {
		if err := validateExpression(expr, opts); err != nil {
			return err
		}
	}
	return nil
}

func validateExpression(expr Expression, opts *ValidateOptions) error {
	if opts != nil && len(opts.AllowedExpressionSchemes) > 0 {
		if !slices.Contains(opts.AllowedExpressionSchemes, expr.Scheme) {
			return fmt.Errorf("expression scheme %q is not allowed", expr.Scheme)
		}
	}
	if opts != nil && opts.ExpressionValidators != nil {
		if validator, ok := opts.ExpressionValidators[expr.Scheme]; ok && validator != nil {
			if err := validator(expr.Target); err != nil {
				return fmt.Errorf("invalid %s expression target %q: %w", expr.Scheme, expr.Target, err)
			}
			return nil
		}
	}
	switch expr.Scheme {
	case "env":
		if strings.ContainsAny(expr.Target, " \t\r\n=") {
			return fmt.Errorf("invalid env expression target %q: environment variable name cannot contain whitespace or '='", expr.Target)
		}
	case "file":
		if strings.TrimSpace(expr.Target) == "" {
			return fmt.Errorf("invalid file expression target: path cannot be empty")
		}
	}
	return nil
}

func isValidExpressionScheme(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if i == 0 {
			if c < 'a' || c > 'z' {
				return false
			}
			continue
		}
		isLower := c >= 'a' && c <= 'z'
		isDigit := c >= '0' && c <= '9'
		isPunct := c == '+' || c == '.' || c == '-'
		if !isLower && !isDigit && !isPunct {
			return false
		}
	}
	return true
}
