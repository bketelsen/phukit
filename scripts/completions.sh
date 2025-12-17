#!/bin/sh
set -e
rm -rf completions
mkdir completions
go build -o phukit .
for sh in bash zsh fish; do
  ./phukit completion "$sh" >"completions/phukit.$sh"
done