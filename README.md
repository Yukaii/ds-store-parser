# ds-store-parser

`ds-store-parser` is a command-line tool that parses Apple's `.DS_Store` files and prints a human-readable interpretation of their contents. This can help you understand what metadata macOS is storing for directories.

## Installation

### Using Homebrew (macOS)

First, tap the repository (if hosted in a separate tap repository, adjust accordingly):

```bash
brew tap Yukaii/ds-store-parser
```

Then install:

```bash
brew install ds-store-parser
```

### Using go install

If you have Go installed:

```bash
go install github.com/Yukaii/ds-store-parser/cmd/ds-store-parser@latest
```

Ensure that your GOPATH/bin (or Go bin directory) is in your PATH:

```bash
export PATH=$PATH:$(go env GOPATH)/bin
```

## Usage

```bash
ds-store-parser path/to/.DS_Store
```

If no argument is specified, it attempts to parse .DS_Store in the current directory.

Example:

```bash
ds-store-parser .DS_Store
```

This will print out records and their interpreted meanings, such as window layout preferences, icon positions, background settings, etc.

## License

MIT
