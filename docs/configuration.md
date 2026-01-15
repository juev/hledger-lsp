# Configuration

The server reads settings from the `hledger` section of your LSP client configuration.

## Limits

- `hledger.limits.maxFileSizeBytes` (default: `10485760`)  
  Maximum journal file size in bytes used by the include loader.
- `hledger.limits.maxIncludeDepth` (default: `50`)  
  Maximum include depth for recursive loading.

## Completion

- `hledger.completion.maxResults` (default: `50`)  
  Maximum number of completion items returned.
