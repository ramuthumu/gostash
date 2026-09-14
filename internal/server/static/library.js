(function () {
  "use strict";
  var savedY = null;
  document.addEventListener("htmx:beforeSwap", function (event) {
    if (event.detail.target.id !== "library") return;
    if (event.detail.requestConfig.verb === "post") savedY = window.scrollY;
  });
  document.addEventListener("htmx:afterSwap", function (event) {
    if (event.detail.target.id !== "library" || savedY === null) return;
    window.scrollTo({ top: savedY, behavior: "instant" });
    savedY = null;
  });
  function showError() {
    var message = document.querySelector("#library .request-error");
    if (message) message.hidden = false;
  }
  document.addEventListener("htmx:responseError", showError);
  document.addEventListener("htmx:sendError", showError);
})();
