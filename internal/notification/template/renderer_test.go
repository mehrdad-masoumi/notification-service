package template_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"notification-service/internal/notification/template"
)

func TestRender_Basic(t *testing.T) {
	out, err := template.Render("Hello {{name}}, your code is {{code}}.", map[string]any{
		"name": "Ali",
		"code": 1234,
	})
	require.NoError(t, err)
	require.Equal(t, "Hello Ali, your code is 1234.", out)
}

func TestRender_MissingVariables(t *testing.T) {
	_, err := template.Render("Amount: {{amount}} {{currency}}", map[string]any{
		"amount": 100,
	})
	require.Error(t, err)
	var missErr *template.MissingVariableError
	require.ErrorAs(t, err, &missErr)
	require.Contains(t, missErr.Variables, "currency")
}

func TestRender_EmptyVariablesMap(t *testing.T) {
	out, err := template.Render("static text with no placeholders", nil)
	require.NoError(t, err)
	require.Equal(t, "static text with no placeholders", out)

	_, err = template.Render("{{missing}}", nil)
	require.Error(t, err)
}

func TestRender_NoCodeExecution(t *testing.T) {
	// Anything other than a plain identifier inside {{ }} is left untouched:
	// there is no expression evaluation.
	out, err := template.Render("value: {{1+1}}", nil)
	require.NoError(t, err)
	require.Equal(t, "value: {{1+1}}", out)
}

func TestVariables_Dedupe(t *testing.T) {
	vars := template.Variables("{{a}} and {{b}} and {{a}} again")
	require.Equal(t, []string{"a", "b"}, vars)
}

func TestRenderPair(t *testing.T) {
	subject, body, err := template.RenderPair("Hi {{name}}", "Body {{name}} {{amount}}", map[string]any{
		"name":   "Sara",
		"amount": 10,
	})
	require.NoError(t, err)
	require.Equal(t, "Hi Sara", subject)
	require.Equal(t, "Body Sara 10", body)
}

func TestRenderPair_MissingCombined(t *testing.T) {
	_, _, err := template.RenderPair("Hi {{name}}", "Body {{amount}}", nil)
	require.Error(t, err)
	var missErr *template.MissingVariableError
	require.ErrorAs(t, err, &missErr)
	require.ElementsMatch(t, []string{"name", "amount"}, missErr.Variables)
}
