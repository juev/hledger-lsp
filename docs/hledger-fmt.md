# hledger-fmt

`hledger-fmt` formats hledger journal files without a running language server. It uses the same formatter as `textDocument/formatting`, with the same defaults, so its output matches format-on-save in an editor using the default settings.

```plain
hledger-fmt [flags] [file ...]
```

With no files, or with `-`, it reads standard input. Without `-w` or `-c`, it writes the formatted journal to standard output, and several files are rejected.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-w`, `--write` | | Write the result back to each file. Files already formatted are not touched. |
| `-c`, `--check` | | List unformatted files and exit 1 if there are any. Changes nothing. |
| `--root <journal>` | | Root journal that includes the files. See [Commodity formats](#commodity-formats). |
| `--indent-size <n>` | `4` | Same as `hledger.formatting.indentSize` |
| `--no-align` | | Same as `hledger.formatting.alignAmounts: false` |
| `--min-alignment-column <n>` | `0` | Same as `hledger.formatting.minAlignmentColumn` |
| `--amount-alignment-column <n>` | `0` | Same as `hledger.formatting.amountAlignmentColumn` |
| `--amount-alignment-mode <mode>` | `right` | Same as `hledger.formatting.amountAlignmentMode`: `left`, `right` or `decimal` |
| `--amount-alignment-target <target>` | `cost` | Same as `hledger.formatting.amountAlignmentTarget`: `cost` or `posting` |
| `-v`, `--version` | | Print the version. Must be the first argument. |

Flags go before file names. Editor settings are not read, so a non-default configuration has to be repeated as flags. See [Configuration](configuration.md#formatting) for what each setting does.

## Exit status

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | `--check` found unformatted files |
| `2` | An error, such as bad arguments, an unreadable file or a parse error |

## Behavior

- **Parse errors:** a journal that does not parse is reported as `file:line:column: message` and left untouched. Unlike an editor, which formats what it could parse and has undo, `hledger-fmt` rewrites nothing it cannot fully read. Other files on the command line are still processed.
- **Rules files:** `.rules` files are refused, because they are not journals.
- **Line endings:** a file keeps its dominant line ending, so CRLF journals stay CRLF.
- **Writes:** `-w` follows symlinks and refuses files it has no permission to write, so a journal kept read-only stays untouched. It replaces a file atomically, keeping its permission, set-user-ID, set-group-ID and sticky bits, its owner and its group. A file the new copy could not match is written in place instead, so its links, owner and attributes stay the same, and its old contents are kept in a backup next to it until the write succeeds. That is a file with other hard links, a file whose owner or mode bits cannot be kept (for example one owned by another user when not run as root), and a file with extended attributes, which on Linux also hold ACLs and SELinux labels. Only Linux is checked for extended attributes; on other systems every file is written in place. Other metadata, such as timestamps, is not kept.
- **Byte order mark:** a journal that starts with a UTF-8 byte order mark keeps it.

## Commodity formats

Commodity directives affect how amounts are written. They are read from the file being formatted and everything it includes. A file that only gets its directives from a parent journal needs that parent, given with `--root`:

```bash
hledger-fmt -w --root main.journal 2023.journal 2024.journal
```

A file that `--root` does not include is an error, and `--root` cannot be used with standard input, which is not part of any include tree.

Amounts are read the way hledger reads them where the file is included, so a number keeps its value when it is rewritten in its commodity's style: with `commodity 0,000.000 FDRXX`, `1,234 FDRXX` becomes `1,234.000 FDRXX`. A file included several times under different directives, such as two `decimal-mark` settings, can mean different things at each inclusion; its numbers are then kept as written and only the layout is formatted. This is the same as formatting in an editor.

A failed `include` is only a warning. A commodity declared in that file has no known style, so its amounts keep their written form.
