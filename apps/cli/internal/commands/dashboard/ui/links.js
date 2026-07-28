function renderLinks(data) {
  const links = data.links || [];
  const el = $("links-list");
  el.innerHTML = links.map((link) => `<div class="link-card"><div><strong>${escapeHtml(link.name)}</strong><div class="meta">${escapeHtml(link.kind)}</div></div><a href="${escapeHtml(link.url)}" target="_blank" rel="noreferrer">Open</a></div>`).join("");
}
