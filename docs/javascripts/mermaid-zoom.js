// Mermaid diagram zoom functionality
document.addEventListener("DOMContentLoaded", function () {
  // Create modal element
  const modal = document.createElement("div");
  modal.className = "diagram-modal";
  modal.innerHTML =
    '<span class="diagram-modal-close">&times;</span><div class="diagram-modal-content"></div>';
  document.body.appendChild(modal);

  const modalContent = modal.querySelector(".diagram-modal-content");
  const closeBtn = modal.querySelector(".diagram-modal-close");

  // Close modal on click
  modal.addEventListener("click", function (e) {
    if (e.target === modal || e.target === closeBtn) {
      modal.classList.remove("active");
      document.body.style.overflow = "";
    }
  });

  // Close on escape key
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape" && modal.classList.contains("active")) {
      modal.classList.remove("active");
      document.body.style.overflow = "";
    }
  });

  // Add click handler to all mermaid diagrams
  function addZoomHandlers() {
    document.querySelectorAll(".mermaid").forEach(function (diagram) {
      if (diagram.dataset.zoomEnabled) return;
      diagram.dataset.zoomEnabled = "true";

      diagram.addEventListener("click", function () {
        const svg = diagram.querySelector("svg");
        if (svg) {
          modalContent.innerHTML = svg.outerHTML;
          modal.classList.add("active");
          document.body.style.overflow = "hidden";
        }
      });
    });
  }

  // Initial setup
  addZoomHandlers();

  // Re-run after navigation (for MkDocs instant loading)
  if (typeof document$ !== "undefined") {
    document$.subscribe(function () {
      setTimeout(addZoomHandlers, 500);
    });
  }

  // Also observe for dynamically added diagrams
  const observer = new MutationObserver(function (mutations) {
    mutations.forEach(function (mutation) {
      if (mutation.addedNodes.length) {
        setTimeout(addZoomHandlers, 100);
      }
    });
  });

  observer.observe(document.body, { childList: true, subtree: true });
});
