#!/usr/bin/env bash

# Iterate through all .go files in the current directory
for file in *.go; do
  # Skip if no .go files exist
  [ -e "$file" ] || continue

  echo "File: $file"
  # Use grep with regex to match function declarations
  # Handles both exported and unexported functions, with or without receivers
  grep -E '^[[:space:]]*func[[:space:]]*\(.*\)|^[[:space:]]*func[[:space:]]+[A-Za-z0-9_]+' "$file"
  echo
done
