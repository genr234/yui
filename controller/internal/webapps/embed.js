// Yui embed helper, served at /_yui/embed.js on every app origin
// (contracts §6). It is the page's side of the channel to its app.
(function () {
  "use strict";
  if (window.yui && window.yui.__embed) return;

  var parentWindow = window.parent;
  var handlers = { message: [], input: [], players: [] };
  var resolveReady;
  var ready = new Promise(function (resolve) {
    resolveReady = resolve;
  });

  function emit(kind, value) {
    var list = handlers[kind].slice();
    for (var i = 0; i < list.length; i++) {
      try {
        list[i](value);
      } catch (error) {
        console.error("[yui] handler failed", error);
      }
    }
  }

  function subscribe(kind, handler) {
    if (typeof handler !== "function") throw new TypeError("handler must be a function");
    handlers[kind].push(handler);
    return function off() {
      var index = handlers[kind].indexOf(handler);
      if (index >= 0) handlers[kind].splice(index, 1);
    };
  }

  window.addEventListener("message", function (event) {
    if (event.source !== parentWindow) return;
    var message = event.data;
    if (!message || message.yuiEmbed !== 1) return;
    switch (message.type) {
      case "ready":
        resolveReady();
        break;
      case "message":
        emit("message", message.data);
        break;
      case "input":
        emit("input", message.frame);
        break;
      case "players":
        emit("players", message.players);
        break;
    }
  });

  function send(message, transfer) {
    // The overlay checks event.source and our origin; "*" is fine because
    // only our parent window can receive it.
    parentWindow.postMessage(message, "*", transfer || []);
  }

  window.yui = {
    __embed: true,
    ready: ready,
    post: function (data, transfer) {
      send({ yuiEmbed: 1, type: "message", data: data }, transfer);
    },
    on: function (handler) {
      return subscribe("message", handler);
    },
    onInput: function (handler) {
      return subscribe("input", handler);
    },
    onPlayers: function (handler) {
      return subscribe("players", handler);
    },
  };

  if (parentWindow && parentWindow !== window) send({ yuiEmbed: 1, type: "ready" });
})();
