#!/usr/bin/env bash
set -e

# 1. Format Go code
if command -v go >/dev/null 2>&1; then
  echo "💧 Formatting Go code..."
  go fmt ./...
else
  echo "⚠️  'go' not found, skipping Go formatting."
fi

# 2. Format Python code (prefers ruff, falls back to uvx ruff)
if command -v ruff >/dev/null 2>&1; then
  echo "🐍 Formatting Python code (ruff)..."
  find . -name "*.py" -not -path "*/.*" -not -path "*/.venv/*" -exec ruff format {} +
elif command -v uvx >/dev/null 2>&1; then
  echo "🐍 Formatting Python code (uvx ruff)..."
  find . -name "*.py" -not -path "*/.*" -not -path "*/.venv/*" -exec uvx ruff format {} +
else
  echo "⚠️  Neither 'ruff' nor 'uvx' found. Skipping Python formatting."
fi

# 3. Format web and Markdown files with Prettier (.js, .css, .html, .md)
if command -v prettier >/dev/null 2>&1; then
  echo "✨ Formatting files (.js, .css, .html, .md with prettier)..."
  find . \( -name "*.js" -o -name "*.css" -o -name "*.html" -o -name "*.md" \) -not -path "*/.*" -not -path "*/node_modules/*" -exec prettier --write {} +
elif command -v npx >/dev/null 2>&1; then
  echo "✨ Formatting files (.js, .css, .html, .md with npx prettier)..."
  find . \( -name "*.js" -o -name "*.css" -o -name "*.html" -o -name "*.md" \) -not -path "*/.*" -not -path "*/node_modules/*" -exec npx --yes prettier --write {} +
else
  echo "⚠️  Neither 'prettier' nor 'npx' found. Skipping Prettier formatting."
fi

# 4. Format Shell scripts (prefers native shfmt from brew, falls back to uvx)
if command -v shfmt >/dev/null 2>&1; then
  echo "📜 Formatting shell scripts (shfmt)..."
  find . -name "*.sh" -not -path "*/.*" -exec shfmt -i 2 -w {} +
elif command -v uvx >/dev/null 2>&1; then
  echo "📜 Formatting shell scripts (uvx shfmt-py)..."
  find . -name "*.sh" -not -path "*/.*" -exec uvx --from shfmt-py shfmt -i 2 -w {} +
else
  echo "⚠️  Neither 'shfmt' nor 'uvx' found. Install with: brew install shfmt"
fi

echo "✅ Done!"
