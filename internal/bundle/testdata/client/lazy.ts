export function mount() {
  document.body.dataset.mounted = String(process.env.NODE_ENV)
}
