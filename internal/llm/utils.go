package llm

import "strings"

// RemoveMarkdownCodeBlocks removes markdown code blocks from a text string.
// It handles various formats including ```json, ```, and closing backticks
// that may be on the same line or a separate line.
func RemoveMarkdownCodeBlocks(text string) string {
	text = strings.TrimSpace(text)

	// Remove opening ```json or ```
	if strings.HasPrefix(text, "```json") {
		text = strings.TrimPrefix(text, "```json")
	} else if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```")
	}

	// Remove closing ``` (may be on same line or separate line)
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, "```")
	// Also handle case where closing backticks are on a new line
	text = strings.TrimSpace(text)
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		text = strings.Join(lines[:len(lines)-1], "\n")
	}
	text = strings.TrimSpace(text)

	return text
}
