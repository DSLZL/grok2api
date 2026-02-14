async function loadPublicHeader() {
  const container = document.getElementById('app-header');
  if (!container) return;
  try {
    const res = await fetch('/static/common/public-header.html?v=1');
    if (!res.ok) return;
    container.innerHTML = await res.text();
    const path = window.location.pathname;
    const links = container.querySelectorAll('a[data-nav]');
    links.forEach((link) => {
      const target = link.getAttribute('data-nav') || '';
      if (target && path.startsWith(target)) {
        link.classList.add('active');
      }
    });
  } catch (e) {
    // Fail silently to avoid breaking page load
  }
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', loadPublicHeader);
} else {
  loadPublicHeader();
}
