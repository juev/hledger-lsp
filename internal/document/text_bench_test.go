package document

import (
	"fmt"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
)

func buildJournalLines(transactions int) string {
	var sb strings.Builder
	for i := 0; i < transactions; i++ {
		fmt.Fprintf(&sb, "2024-01-01 * Payee %d | note\n", i)
		fmt.Fprintf(&sb, "    expenses:acct%d  $%d.00\n", i%10, i+1)
		sb.WriteString("    assets:cash\n")
	}
	return sb.String()
}

// BenchmarkTextApplyChange measures the edit-application cost (ApplyChange only;
// the O(n) build happens once, outside the timer) at 1k and 10k transactions. A
// sub-linear O(log n) edit stays roughly flat across the two sizes. Edits
// alternate insert/delete at the same position so the document stays bounded.
func BenchmarkTextApplyChange(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("%dtx", n), func(b *testing.B) {
			content := buildJournalLines(n)
			lineCount := strings.Count(content, "\n") + 1
			mid := uint32(lineCount / 2)
			insert := protocol.Range{
				Start: protocol.Position{Line: mid, Character: 0},
				End:   protocol.Position{Line: mid, Character: 0},
			}
			del := protocol.Range{
				Start: protocol.Position{Line: mid, Character: 0},
				End:   protocol.Position{Line: mid, Character: 1},
			}
			nt := NewText(content)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if i%2 == 0 {
					nt.ApplyChange(insert, "x")
				} else {
					nt.ApplyChange(del, "")
				}
			}
		})
	}
}
