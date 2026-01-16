package parser

import (
	"testing"

	"github.com/juev/hledger-lsp/internal/testutil"
)

var (
	smallJournal  = testutil.GenerateJournal(10)
	mediumJournal = testutil.GenerateJournal(100)
	largeJournal  = testutil.GenerateJournal(1000)
	xlargeJournal = testutil.GenerateJournal(10000)
)

func BenchmarkLexer_Small(b *testing.B) {
	for b.Loop() {
		lexer := NewLexer(smallJournal)
		for {
			tok := lexer.Next()
			if tok.Type == TokenEOF {
				break
			}
		}
	}
}

func BenchmarkLexer_Medium(b *testing.B) {
	for b.Loop() {
		lexer := NewLexer(mediumJournal)
		for {
			tok := lexer.Next()
			if tok.Type == TokenEOF {
				break
			}
		}
	}
}

func BenchmarkLexer_Large(b *testing.B) {
	for b.Loop() {
		lexer := NewLexer(largeJournal)
		for {
			tok := lexer.Next()
			if tok.Type == TokenEOF {
				break
			}
		}
	}
}

func BenchmarkParser_Small(b *testing.B) {
	for b.Loop() {
		Parse(smallJournal)
	}
}

func BenchmarkParser_Medium(b *testing.B) {
	for b.Loop() {
		Parse(mediumJournal)
	}
}

func BenchmarkParser_Large(b *testing.B) {
	for b.Loop() {
		Parse(largeJournal)
	}
}

func BenchmarkParser_Parallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			Parse(mediumJournal)
		}
	})
}

func BenchmarkLexer_XLarge(b *testing.B) {
	for b.Loop() {
		lexer := NewLexer(xlargeJournal)
		for {
			tok := lexer.Next()
			if tok.Type == TokenEOF {
				break
			}
		}
	}
}

func BenchmarkParser_XLarge(b *testing.B) {
	for b.Loop() {
		Parse(xlargeJournal)
	}
}

func BenchmarkParser_Large_Allocs(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		Parse(largeJournal)
	}
}

func BenchmarkParser_XLarge_Allocs(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		Parse(xlargeJournal)
	}
}
