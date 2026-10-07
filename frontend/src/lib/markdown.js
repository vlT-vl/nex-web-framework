const rawHtmlTags = new Set([
  "a", "b", "br", "code", "em", "h1", "h2", "h3", "h4", "h5", "h6",
  "hr", "i", "img", "li", "ol", "p", "picture", "pre", "source", "span",
  "strong", "sub", "sup", "table", "tbody", "td", "th", "thead", "tr", "ul",
]);

const allowedAttrs = new Set([
  "align", "alt", "class", "colspan", "height", "href", "media", "rowspan",
  "src", "srcset", "title", "width",
]);

function escapeHtml(value) {
  return String(value)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function escapeAttr(value) {
  return escapeHtml(value).replace(/`/g, "&#96;");
}

function rewriteAssetPath(value) {
  const raw = String(value || "").trim();
  if (!raw) return "";
  if (raw.startsWith("res/")) return `/${raw.slice(4)}`;
  return raw;
}

function safeUrl(value) {
  const url = rewriteAssetPath(value);
  const lower = url.toLowerCase();
  if (lower === "docs.md" || lower === "readme.md") return "#documentation-scope";
  if (lower.startsWith("docs.md#")) return `#${url.slice(url.indexOf("#") + 1)}`;
  if (
    url.startsWith("/") ||
    url.startsWith("./") ||
    url.startsWith("../") ||
    url.startsWith("#") ||
    url.startsWith("mailto:") ||
    url.startsWith("http://") ||
    url.startsWith("https://")
  ) {
    return url;
  }
  return "#";
}

function safeSrcset(value) {
  return String(value || "")
    .split(",")
    .map((part) => {
      const tokens = part.trim().split(/\s+/);
      if (tokens.length === 0 || !tokens[0]) return "";
      return [safeUrl(tokens[0]), ...tokens.slice(1)].join(" ");
    })
    .filter(Boolean)
    .join(", ");
}

function slugify(value) {
  return String(value)
    .toLowerCase()
    .replace(/<[^>]+>/g, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function isSafeHtmlLine(line) {
  const trimmed = line.trim();
  return /^<\/?[a-z][\w-]*(\s|>|\/>)/i.test(trimmed);
}

function htmlRootTag(line) {
  const match = /^<([a-z][\w-]*)(\s|>|\/>)/i.exec(line.trim());
  if (!match) return "";
  const tag = match[1].toLowerCase();
  if (!rawHtmlTags.has(tag) || /\/>\s*$/.test(line.trim())) return "";
  return tag;
}

function closesHtmlTag(line, tag) {
  return new RegExp(`</${tag}>`, "i").test(line);
}

function sanitizeHtml(html) {
  if (typeof document === "undefined") return escapeHtml(html);

  const template = document.createElement("template");
  template.innerHTML = html;

  function walk(node) {
    if (node.nodeType === Node.COMMENT_NODE) {
      node.remove();
      return;
    }
    if (node.nodeType !== Node.ELEMENT_NODE) return;

    const tag = node.tagName.toLowerCase();
    if (!rawHtmlTags.has(tag)) {
      node.replaceWith(document.createTextNode(node.textContent || ""));
      return;
    }

    for (const attr of Array.from(node.attributes)) {
      const name = attr.name.toLowerCase();
      if (name.startsWith("on") || !allowedAttrs.has(name)) {
        node.removeAttribute(attr.name);
        continue;
      }
      if (name === "href" || name === "src") {
        node.setAttribute(attr.name, safeUrl(attr.value));
      } else if (name === "srcset") {
        node.setAttribute(attr.name, safeSrcset(attr.value));
      }
    }

    for (const child of Array.from(node.childNodes)) walk(child);
  }

  for (const child of Array.from(template.content.childNodes)) walk(child);
  return template.innerHTML;
}

function renderInline(value) {
  const code = [];
  let html = String(value).replace(/`([^`]+)`/g, (_, body) => {
    const key = `@@CODE_${code.length}@@`;
    code.push(`<code>${escapeHtml(body)}</code>`);
    return key;
  });

  html = escapeHtml(html);
  html = html.replace(/!\[([^\]]*)\]\(([^)\s]+)(?:\s+"([^"]+)")?\)/g, (_, alt, src, title) => {
    const titleAttr = title ? ` title="${escapeAttr(title)}"` : "";
    return `<img src="${escapeAttr(safeUrl(src))}" alt="${escapeAttr(alt)}"${titleAttr}>`;
  });
  html = html.replace(/\[([^\]]+)\]\(([^)\s]+)(?:\s+"([^"]+)")?\)/g, (_, text, href, title) => {
    const titleAttr = title ? ` title="${escapeAttr(title)}"` : "";
    return `<a href="${escapeAttr(safeUrl(href))}"${titleAttr}>${text}</a>`;
  });
  html = html.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  html = html.replace(/\*([^*]+)\*/g, "<em>$1</em>");
  html = html.replace(/@@CODE_(\d+)@@/g, (_, index) => code[Number(index)] || "");
  return html;
}

function isTableStart(lines, index) {
  return (
    lines[index]?.includes("|") &&
    /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(lines[index + 1] || "")
  );
}

function splitTableRow(line) {
  return line
    .trim()
    .replace(/^\|/, "")
    .replace(/\|$/, "")
    .split("|")
    .map((cell) => cell.trim());
}

function isBlockStart(lines, index) {
  const line = lines[index] || "";
  return (
    line.trim() === "" ||
    /^```/.test(line) ||
    /^#{1,6}\s+/.test(line) ||
    /^-{3,}\s*$/.test(line.trim()) ||
    /^>\s?/.test(line) ||
    /^\s*[-*]\s+/.test(line) ||
    /^\s*\d+\.\s+/.test(line) ||
    isTableStart(lines, index) ||
    isSafeHtmlLine(line)
  );
}

export function renderMarkdown(markdown) {
  const lines = String(markdown || "").replace(/\r\n?/g, "\n").split("\n");
  const out = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];
    const trimmed = line.trim();

    if (!trimmed) {
      i += 1;
      continue;
    }

    if (/^```/.test(trimmed)) {
      const lang = trimmed.slice(3).trim();
      const body = [];
      i += 1;
      while (i < lines.length && !/^```/.test(lines[i].trim())) {
        body.push(lines[i]);
        i += 1;
      }
      i += 1;
      const classAttr = lang ? ` class="language-${escapeAttr(lang)}"` : "";
      out.push(`<pre><code${classAttr}>${escapeHtml(body.join("\n"))}</code></pre>`);
      continue;
    }

    if (isSafeHtmlLine(line)) {
      const body = [];
      const rootTag = htmlRootTag(line);

      if (rootTag) {
        body.push(lines[i]);
        i += 1;
        while (
          i < lines.length &&
          lines[i].trim() &&
          !closesHtmlTag(body[body.length - 1], rootTag)
        ) {
          body.push(lines[i]);
          i += 1;
        }
      } else {
        while (i < lines.length && lines[i].trim() && isSafeHtmlLine(lines[i])) {
          body.push(lines[i]);
          i += 1;
        }
      }

      out.push(sanitizeHtml(body.join("\n")));
      continue;
    }

    const heading = /^(#{1,6})\s+(.+)$/.exec(line);
    if (heading) {
      const level = heading[1].length;
      const inner = renderInline(heading[2].trim());
      const id = slugify(inner);
      out.push(`<h${level}${id ? ` id="${escapeAttr(id)}"` : ""}>${inner}</h${level}>`);
      i += 1;
      continue;
    }

    if (/^-{3,}\s*$/.test(trimmed)) {
      out.push("<hr>");
      i += 1;
      continue;
    }

    if (isTableStart(lines, i)) {
      const headers = splitTableRow(lines[i]);
      i += 2;
      const rows = [];
      while (i < lines.length && lines[i].includes("|") && lines[i].trim()) {
        rows.push(splitTableRow(lines[i]));
        i += 1;
      }
      out.push([
        "<table>",
        `<thead><tr>${headers.map((cell) => `<th>${renderInline(cell)}</th>`).join("")}</tr></thead>`,
        `<tbody>${rows.map((row) => `<tr>${row.map((cell) => `<td>${renderInline(cell)}</td>`).join("")}</tr>`).join("")}</tbody>`,
        "</table>",
      ].join(""));
      continue;
    }

    if (/^\s*[-*]\s+/.test(line) || /^\s*\d+\.\s+/.test(line)) {
      const ordered = /^\s*\d+\.\s+/.test(line);
      const items = [];
      const itemPattern = ordered ? /^\s*\d+\.\s+(.+)$/ : /^\s*[-*]\s+(.+)$/;
      while (i < lines.length && itemPattern.test(lines[i])) {
        items.push(renderInline(lines[i].replace(itemPattern, "$1")));
        i += 1;
      }
      const tag = ordered ? "ol" : "ul";
      out.push(`<${tag}>${items.map((item) => `<li>${item}</li>`).join("")}</${tag}>`);
      continue;
    }

    if (/^>\s?/.test(line)) {
      const body = [];
      while (i < lines.length && /^>\s?/.test(lines[i])) {
        body.push(lines[i].replace(/^>\s?/, ""));
        i += 1;
      }
      out.push(`<blockquote>${renderInline(body.join(" "))}</blockquote>`);
      continue;
    }

    const paragraph = [line.trim()];
    i += 1;
    while (i < lines.length && !isBlockStart(lines, i)) {
      paragraph.push(lines[i].trim());
      i += 1;
    }
    out.push(`<p>${renderInline(paragraph.join(" "))}</p>`);
  }

  return out.join("\n");
}
