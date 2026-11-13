// Add this script to the deployed Ghost theme's footer (or its equivalent).
// It targets Ghost video cards so the same behavior applies to Lexical cards.
(() => {
  const videos = document.querySelectorAll('.kg-video-card video');
  if (!videos.length) return;

  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)');
  const allowManualPlayback = (video) => {
    video.pause();
    video.removeAttribute('autoplay');
    video.controls = true;
  };

  if (reducedMotion.matches || !('IntersectionObserver' in window)) {
    videos.forEach(allowManualPlayback);
    return;
  }

  const observer = new IntersectionObserver((entries) => {
    entries.forEach((entry) => {
      const video = entry.target;
      if (entry.isIntersecting && entry.intersectionRatio >= 0.5) {
        if (video.dataset.autoplayBlocked === 'true') return;
        video.play().catch(() => {
          video.dataset.autoplayBlocked = 'true';
          video.controls = true;
        });
      } else {
        video.pause();
      }
    });
  }, { threshold: [0, 0.5] });

  videos.forEach((video) => {
    video.muted = true;
    video.loop = true;
    video.playsInline = true;
    video.preload = 'none';
    observer.observe(video);
  });
})();
