import Search from "./search.js";
import Toast from "./toasts.js";
import Modal from "./modal.js";

export default {
  render: async (containerId, searchTerm = "") => {
    const resp = await fetch("/templates/sites.html");
    const templateText = await resp.text();
    const template = Handlebars.compile(templateText);

    let sitesResp = await fetch("/api/sites");
    let sitesData = await sitesResp.json();

    const html = template({ sites: sitesData });
    document.querySelector(containerId).innerHTML = html;

    const sitesList = document.getElementById("sitesList");
    sitesList.addEventListener("click", async (e) => {
      const viewButton = e.target.closest(".view-json");
      if (viewButton) {
        const domain = viewButton.dataset.domain;
        if (!domain) return;

        const siteResp = await fetch(`/api/sites/${domain}`);
        const siteConfig = await siteResp.json();
        const jsonContent = JSON.stringify(siteConfig, null, 2);

        Modal.show({
          id: "site-json-modal",
          title: `${domain}'s JSON`,
          content: `<pre id="json-content" class="whitespace-pre-wrap bg-gray-100 p-2 rounded">${jsonContent}</pre>`,
          buttons: [
            {
              id: "edit-json-btn",
              text: "Edit",
              icon: "edit",
              onClick: () => {
                Modal.setTitle("site-json-modal", `Editing ${domain}'s JSON`);
                Modal.toggleButton("site-json-modal", "edit-json-btn", false);
                Modal.toggleButton("site-json-modal", "save-json-btn", true);
                document.getElementById("json-content").innerHTML = `
                  <textarea id="json-editor" class="w-full h-80 p-2 border rounded">${jsonContent}</textarea>
                `;
              },
            },
            {
              id: "save-json-btn",
              text: "Save Changes",
              hidden: true,
              icon: "save",
              onClick: async () => {
                const newConfig = document.getElementById("json-editor").value;
                await fetch(`/api/sites/${domain}`, {
                  method: "PUT",
                  headers: { "Content-Type": "application/json" },
                  body: newConfig,
                });
                Toast.show(
                  "Configuration updated. Restarting server...",
                  "info",
                  3000
                );
                Modal.hide("site-json-modal");
                setTimeout(() => window.location.reload(), 6000);
              },
            },
          ],
        });
        return;
      }

      const deleteButton = e.target.closest(".delete-site");
      if (deleteButton) {
        const domain = deleteButton.dataset.domain;
        if (confirm(`Are you sure you want to delete ${domain}?`)) {
          await fetch(`/api/sites/${domain}`, { method: "DELETE" });
          Toast.show("Site deleted. Restarting server...", "error", 3000);
          setTimeout(
            () => Toast.show("Server restarted", "success", 3000),
            6000
          );
        }
      }
    });

    window.currentView.applySearch = (searchTerm) => {
      Search.filterList(".divide-y li", searchTerm);
    };
  },
};
