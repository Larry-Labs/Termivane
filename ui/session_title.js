export function sessionPageTitle(tab) {
  if (!tab) return "Termivane";
  const name = (tab.name || "").trim();
  const prefix = tab.status?.disconnected ? "已断开 · " : "";
  return prefix + (name ? name + " · " : "") + "Termivane";
}
