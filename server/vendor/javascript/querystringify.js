// querystringify@2.2.0 downloaded from https://ga.jspm.io/npm:querystringify@2.2.0/index.js

var e={};var n=Object.prototype.hasOwnProperty,r;
/**
 * Decode a URI encoded string.
 *
 * @param {String} input The URI encoded string.
 * @returns {String|Null} The decoded string.
 * @api private
 */function decode(e){try{return decodeURIComponent(e.replace(/\+/g," "))}catch(e){return null}}
/**
 * Attempts to encode a given input.
 *
 * @param {String} input The string that needs to be encoded.
 * @returns {String|Null} The encoded string.
 * @api private
 */function encode(e){try{return encodeURIComponent(e)}catch(e){return null}}
/**
 * Simple query string parser.
 *
 * @param {String} query The query string that needs to be parsed.
 * @returns {Object}
 * @api public
 */function querystring(e){var n=/([^=?#&]+)=?([^&]*)/g,r={},t;while(t=n.exec(e)){var o=decode(t[1]),u=decode(t[2]);null===o||null===u||o in r||(r[o]=u)}return r}
/**
 * Transform a query string to an object.
 *
 * @param {Object} obj Object that should be transformed.
 * @param {String} prefix Optional prefix.
 * @returns {String}
 * @api public
 */function querystringify(e,t){t=t||"";var o=[],u,i;"string"!==typeof t&&(t="?");for(i in e)if(n.call(e,i)){u=e[i];u||null!==u&&u!==r&&!isNaN(u)||(u="");i=encode(i);u=encode(u);if(null===i||null===u)continue;o.push(i+"="+u)}return o.length?t+o.join("&"):""}e.stringify=querystringify;e.parse=querystring;const t=e.stringify,o=e.parse;export default e;export{o as parse,t as stringify};

