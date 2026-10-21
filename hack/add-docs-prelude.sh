#!/usr/bin/env bash
# The Hostinger Terraform provider documents its resources without the YAML
# front matter (subcategory, page_title, description) that the Terraform
# registry and upjet's metadata scraper expect. This script adds it to every
# resource document in the given directory. Documents that already start with
# front matter are left untouched, so it is safe to run repeatedly.
set -euo pipefail

docs_dir="${1:?usage: $0 <terraform-docs-resources-dir>}"

for doc in "${docs_dir}"/*.md; do
  [ -e "${doc}" ] || continue
  if [ "$(head -n 1 "${doc}")" = "---" ]; then
    continue
  fi

  name="$(basename "${doc}" .md)"
  case "${name}" in
    dns_*) subcategory="DNS" ;;
    *) subcategory="VPS" ;;
  esac
  # The description is the first paragraph after the "# hostinger_<name>" title.
  description="$(awk 'NR > 1 && NF { print; exit }' "${doc}")"

  tmp="$(mktemp)"
  {
    echo "---"
    echo "subcategory: \"${subcategory}\""
    echo "page_title: \"Hostinger: hostinger_${name}\""
    echo "description: |-"
    echo "  ${description}"
    echo "---"
    echo
    cat "${doc}"
  } > "${tmp}"
  mv "${tmp}" "${doc}"
done
