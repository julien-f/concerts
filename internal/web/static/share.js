// « Partager »: the device's share sheet where there is one, else the link's wa.me (ADR 0010).
document.addEventListener("click", (event) => {
  const link = event.target.closest("a[data-share-text]");
  if (!link || !navigator.share) return;
  event.preventDefault();
  // Rejects when the user closes the sheet: nothing to do.
  navigator.share({ text: link.dataset.shareText, url: link.dataset.shareUrl }).catch(() => {});
});
