const status = document.getElementById('copy-status')
const fallback = document.getElementById('copy-fallback')

for (const link of document.querySelectorAll('[data-copy-key]')) {
  link.addEventListener('click', async (event) => {
    if (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || !navigator.clipboard) {
      return
    }
    event.preventDefault()
    status.textContent = ''
    fallback.hidden = true
    fallback.value = ''
    const key = fetch(link.href, { mode: 'same-origin', redirect: 'error', cache: 'no-store' })
      .then(async (response) => {
        if (!response.ok) throw new Error('Key unavailable')
        const text = await response.text()
        if (!text.startsWith('vpn://')) throw new Error('Invalid key')
        return text
      })

    try {
      if (window.ClipboardItem && navigator.clipboard.write) {

        const content = key.then((text) => new Blob([text], { type: 'text/plain' }))

        content.catch(() => {})
        await navigator.clipboard.write([new ClipboardItem({ 'text/plain': content })])
      } else {
        await navigator.clipboard.writeText(await key)
      }
      status.textContent = 'Ключ скопирован.'
    } catch {

      try {
        fallback.value = await key
        fallback.hidden = false
        fallback.focus()
        fallback.select()
        status.textContent = 'Скопируйте выделенный ключ.'
      } catch {
        status.textContent = 'Ключ пока недоступен. Обновите страницу и попробуйте снова.'
      }
    }
  })
}
