package warp

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseExpression(t *testing.T) {
	valid := []struct {
		input  string
		scheme string
		target string
	}{
		{"${env:OPENAI_API_KEY}", "env", "OPENAI_API_KEY"},
		{"${file:~/.secrets/idp-secret}", "file", "~/.secrets/idp-secret"},
		{"${keychain:acme/mcp-token}", "keychain", "acme/mcp-token"},
		{"${vault:secret/data/mcp#token}", "vault", "secret/data/mcp#token"},
		{"${aws.ssm:/prod/mcp/secret}", "aws.ssm", "/prod/mcp/secret"},
	}
	for _, tc := range valid {
		expr, ok := ParseExpression(tc.input)
		if !ok {
			t.Fatalf("expected ParseExpression(%q) to succeed", tc.input)
		}
		if expr.Raw != tc.input || expr.Scheme != tc.scheme || expr.Target != tc.target {
			t.Errorf("ParseExpression(%q) = %+v, want scheme=%q target=%q", tc.input, expr, tc.scheme, tc.target)
		}
		if !IsExpression(tc.input) {
			t.Errorf("IsExpression(%q) = false, want true", tc.input)
		}
	}

	invalid := []string{
		"",
		"sk-plaintext",
		"${OPENAI_API_KEY}",
		"${env:}",
		"${env:   }",
		"${ENV:FOO}",
		"${1bad:FOO}",
		"${env:FOO} extra",
		"prefix ${env:FOO}",
		"${env:${nested}}",
		"${env:line\nbreak}",
	}
	for _, s := range invalid {
		if _, ok := ParseExpression(s); ok {
			t.Errorf("expected ParseExpression(%q) to fail", s)
		}
	}
}

func TestValidateExpression_And_Extract(t *testing.T) {
	if err := ValidateExpression("${env:MY_VAR}", nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := ValidateExpression("${env:BAD VAR}", nil); err == nil {
		t.Error("expected error for env var with space")
	}
	if err := ValidateExpression("${env:BAD=VAR}", nil); err == nil {
		t.Error("expected error for env var with '='")
	}
	if err := ValidateExpression("${file:~/.secrets/key}", nil); err != nil {
		t.Errorf("unexpected error for file expression: %v", err)
	}
	if err := ValidateExpression("raw-secret", nil); err == nil {
		t.Error("expected plaintext literal to be rejected")
	}

	// Embedded expressions in non-secret strings
	if err := ValidateStringWithExpressions("https://acme.okta.com", nil); err != nil {
		t.Errorf("expected literal URL to pass: %v", err)
	}
	if err := ValidateStringWithExpressions("https://${env:AUTH_HOST}/oauth2/${env:TENANT}", nil); err != nil {
		t.Errorf("expected embedded valid expressions to pass: %v", err)
	}
	if err := ValidateStringWithExpressions("https://${env:UNCLOSED", nil); err == nil {
		t.Error("expected unclosed expression error")
	}
	if err := ValidateStringWithExpressions("https://${bad}/oauth", nil); err == nil {
		t.Error("expected malformed expression error")
	}

	// Custom validator overriding built-in or adding new scheme
	opts := &ValidateOptions{
		ExpressionValidators: map[string]ExpressionValidator{
			"op": func(target string) error {
				if !strings.HasPrefix(target, "op://") {
					return fmt.Errorf("expected op:// reference")
				}
				return nil
			},
		},
	}
	if err := ValidateExpression("${op:Private/MCP/token}", opts); err == nil {
		t.Error("expected custom op validator failure")
	}
	if err := ValidateExpression("${op:op://Private/MCP/token}", opts); err != nil {
		t.Errorf("expected valid op expression to pass: %v", err)
	}
}
