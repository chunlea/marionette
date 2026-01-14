// Mermaid diagram zoom functionality for MkDocs Material
(function () {
  // Create modal element once
  function createModal() {
    if (document.getElementById("diagram-zoom-modal")) return;

    const modal = document.createElement("div");
    modal.id = "diagram-zoom-modal";
    modal.className = "diagram-modal";
    modal.innerHTML = `
      <span class="diagram-modal-close">&times;</span>
      <div class="diagram-modal-content"></div>
    `;
    document.body.appendChild(modal);

    // Close on click outside or on close button
    modal.addEventListener("click", function (e) {
      if (
        e.target === modal ||
        e.target.classList.contains("diagram-modal-close")
      ) {
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

    return modal;
  }

  // Add zoom to a single diagram
  function addZoom(diagram) {
    if (diagram.dataset.zoomEnabled === "true") return;

    const svg = diagram.querySelector("svg");
    if (!svg) return;

    diagram.dataset.zoomEnabled = "true";
    diagram.style.cursor = "zoom-in";

    diagram.addEventListener("click", function (e) {
      e.preventDefault();
      const modal = document.getElementById("diagram-zoom-modal");
      const content = modal.querySelector(".diagram-modal-content");

      // Clone the SVG for the modal
      const svgClone = svg.cloneNode(true);
      svgClone.style.maxWidth = "90vw";
      svgClone.style.maxHeight = "90vh";
      svgClone.style.width = "auto";
      svgClone.style.height = "auto";

      content.innerHTML = "";
      content.appendChild(svgClone);
      modal.classList.add("active");
      document.body.style.overflow = "hidden";
    });
  }

  // Find and setup all mermaid diagrams
  function setupZoom() {
    createModal();
    document.querySelectorAll(".mermaid").forEach(addZoom);
  }

  // Run on page load
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", function () {
      setTimeout(setupZoom, 1000);
    });
  } else {
    setTimeout(setupZoom, 1000);
  }

  // Re-run after MkDocs instant navigation
  if (typeof document$ !== "undefined") {
    document$.subscribe(function () {
      setTimeout(setupZoom, 1000);
    });
  }

  // Watch for dynamically rendered diagrams
  const observer = new MutationObserver(function (mutations) {
    let hasNewDiagrams = false;
    mutations.forEach(function (mutation) {
      mutation.addedNodes.forEach(function (node) {
        if (node.nodeType === 1) {
          if (node.classList && node.classList.contains("mermaid")) {
            hasNewDiagrams = true;
          } else if (node.querySelector && node.querySelector(".mermaid svg")) {
            hasNewDiagrams = true;
          }
        }
      });
    });
    if (hasNewDiagrams) {
      setTimeout(setupZoom, 500);
    }
  });

  // Start observing when DOM is ready
  function startObserver() {
    observer.observe(document.body, {
      childList: true,
      subtree: true,
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", startObserver);
  } else {
    startObserver();
  }
})();
