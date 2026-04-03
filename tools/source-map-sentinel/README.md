# source-map-sentinel

A defensive CLI utility that scans packaged build artifacts for accidental source exposure.

Detects:
- `.map` files (JavaScript/CSS source maps) left in distribution packages
- `sourceMappingURL` references in `.js`, `.css`, `.ts`, and related files

## Installation

```bash
cd tools/source-map-sentinel
go build -o sentinel .
```

## Usage

```bash
# Human-readable output
sentinel ./dist

# Machine-readable JSON
sentinel --json ./dist

# CI usage — exits with code 1 if findings exist
sentinel --fail-on-findings ./dist
```

## Sample output

```
Found 2 issue(s):

  [MAP FILE]           dist/bundle.js.map
  [SOURCE_MAPPING_URL] dist/bundle.js:47
    //# sourceMappingURL=bundle.js.map
```

## CI integration (GitHub Actions)

```yaml
- name: Check for source map exposure
  run: |
    cd tools/source-map-sentinel
    go build -o sentinel .
    ./sentinel --fail-on-findings ../../dist
```

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | No findings (or findings exist but --fail-on-findings not set) |
| 1 | Findings exist and --fail-on-findings was set |
| 2 | Usage error or scan error |
