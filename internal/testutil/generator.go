package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var accounts = []string{
	"expenses:food:groceries",
	"expenses:food:restaurants",
	"expenses:transport:fuel",
	"expenses:utilities:electricity",
	"expenses:utilities:water",
	"assets:bank:checking",
	"assets:bank:savings",
	"assets:cash",
	"liabilities:credit:visa",
	"income:salary",
}

var commodities = []string{"$", "EUR", "RUB"}

func GenerateJournal(numTransactions int) string {
	var sb strings.Builder

	for i := range numTransactions {
		year := 2020 + (i / 365)
		month := (i/30)%12 + 1
		day := i%28 + 1

		fromAcc := accounts[i%len(accounts)]
		toAcc := accounts[(i+1)%len(accounts)]
		commodity := commodities[i%len(commodities)]
		amount := (i%1000 + 1) * 10

		fmt.Fprintf(&sb, "%04d-%02d-%02d * Payee %d | Transaction note\n", year, month, day, i)
		fmt.Fprintf(&sb, "    %s  %s%d.%02d\n", fromAcc, commodity, amount/100, amount%100)

		if i%5 == 0 {
			fmt.Fprintf(&sb, "    %s  %s%d.%02d @ $1.10\n", toAcc, commodity, amount/100, amount%100)
			sb.WriteString("    assets:cash\n")
		} else {
			fmt.Fprintf(&sb, "    %s\n", toAcc)
		}

		if i%10 == 0 {
			fmt.Fprintf(&sb, "    ; tag:value%d\n", i)
		}

		sb.WriteString("\n")
	}

	return sb.String()
}

func GenerateIncludeTree(tmpDir string, numFiles, txPerFile int) (string, error) {
	var mainContent strings.Builder

	for i := range numFiles {
		filename := fmt.Sprintf("file%d.journal", i)
		fmt.Fprintf(&mainContent, "include %s\n", filename)

		content := GenerateJournal(txPerFile)
		filePath := filepath.Join(tmpDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return "", err
		}
	}

	mainPath := filepath.Join(tmpDir, "main.journal")
	if err := os.WriteFile(mainPath, []byte(mainContent.String()), 0644); err != nil {
		return "", err
	}

	return mainPath, nil
}
