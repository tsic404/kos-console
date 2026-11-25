// The one HTML escape the page shell uses: every value that reaches the
// document is escaped here, so a target name or an output line can never open
// or close a tag (KOSD-C-14).

// escapeHTML renders one value as text inside the document.
export function escapeHTML(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}
