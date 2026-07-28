// Package template provides safe, non-executable string substitution for
// notification templates. Only `{{var}}` placeholders are supported; there
// is no expression language, no code execution, and no control flow.
package template

import (
	"fmt"
	"regexp"
	"strings"
)

var placeholderPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_]+)\s*\}\}`)

// MissingVariableError is returned when a template references a variable
// that was not supplied in the variables map.
type MissingVariableError struct {
	Variables []string
}

func (e *MissingVariableError) Error() string {
	return fmt.Sprintf("template: missing variables: %s", strings.Join(e.Variables, ", "))
}

// Variables returns the set of variable names referenced by the template body.
func Variables(body string) []string {
	matches := placeholderPattern.FindAllStringSubmatch(body, -1)
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		name := m[1]
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// Render substitutes `{{var}}` placeholders in body with values from vars.
// Values are converted to their string form via fmt.Sprint. If any
// placeholder has no corresponding entry in vars, a *MissingVariableError is
// returned and no partial output is produced.
func Render(body string, vars map[string]any) (string, error) {
	if vars == nil {
		vars = map[string]any{}
	}

	var missing []string
	for _, name := range Variables(body) {
		if _, ok := vars[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return "", &MissingVariableError{Variables: missing}
	}

	rendered := placeholderPattern.ReplaceAllStringFunc(body, func(match string) string {
		sub := placeholderPattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		return fmt.Sprint(vars[sub[1]])
	})
	return rendered, nil
}

// RenderPair renders subject and body together, returning a single combined
// error listing any variables missing from either.
func RenderPair(subject, body string, vars map[string]any) (renderedSubject, renderedBody string, err error) {
	renderedSubject, subjErr := Render(subject, vars)
	renderedBody, bodyErr := Render(body, vars)

	var missing []string
	if me, ok := subjErr.(*MissingVariableError); ok {
		missing = append(missing, me.Variables...)
	} else if subjErr != nil {
		return "", "", subjErr
	}
	if me, ok := bodyErr.(*MissingVariableError); ok {
		missing = append(missing, me.Variables...)
	} else if bodyErr != nil {
		return "", "", bodyErr
	}
	if len(missing) > 0 {
		return "", "", &MissingVariableError{Variables: dedupe(missing)}
	}
	return renderedSubject, renderedBody, nil
}

func dedupe(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
