// url-parse@1.5.10 downloaded from https://ga.jspm.io/npm:url-parse@1.5.10/index.js

import*as e from"requires-port";import*as o from"querystringify";var t="default"in e?e.default:e;var s="default"in o?o.default:o;var r="undefined"!==typeof globalThis?globalThis:"undefined"!==typeof self?self:global;var a={};var n=t,l=s,i=/^[\x00-\x20\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000\ufeff]+/,p=/[\n\r\t]/g,c=/^[A-Za-z][A-Za-z0-9+-.]*:\/\//,f=/:\d+$/,h=/^([a-z][a-z0-9.+-]*:)?(\/\/)?([\\/]+)?([\S\s]*)/i,u=/^[a-zA-Z]:/;
/**
 * Remove control characters and whitespace from the beginning of a string.
 *
 * @param {Object|String} str String to trim.
 * @returns {String} A new string representing `str` stripped of control
 *     characters and whitespace from its beginning.
 * @public
 */function trimLeft(e){return(e||"").toString().replace(i,"")}var d=[["#","hash"],["?","query"],function sanitize(e,o){return isSpecial(o.protocol)?e.replace(/\\/g,"/"):e},["/","pathname"],["@","auth",1],[NaN,"host",void 0,1,1],[/:(\d*)$/,"port",void 0,1],[NaN,"hostname",void 0,1,1]];
/**
 * These properties should not be copied or inherited from. This is only needed
 * for all non blob URL's as a blob URL does not include a hash, only the
 * origin.
 *
 * @type {Object}
 * @private
 */var m={hash:1,query:1};
/**
 * The location object differs when your code is loaded through a normal page,
 * Worker or through a worker using a blob. And with the blobble begins the
 * trouble as the location object will contain the URL of the blob, not the
 * location of the page where our code is loaded in. The actual origin is
 * encoded in the `pathname` so we can thankfully generate a good "default"
 * location from it so we can generate proper relative URL's again.
 *
 * @param {Object|String} loc Optional default location object.
 * @returns {Object} lolcation object.
 * @public
 */function lolcation(e){var o;o="undefined"!==typeof window?window:"undefined"!==typeof r?r:"undefined"!==typeof self?self:{};var t=o.location||{};e=e||t;var s,a={},n=typeof e;if("blob:"===e.protocol)a=new Url(unescape(e.pathname),{});else if("string"===n){a=new Url(e,{});for(s in m)delete a[s]}else if("object"===n){for(s in e)s in m||(a[s]=e[s]);void 0===a.slashes&&(a.slashes=c.test(e.href))}return a}
/**
 * Check whether a protocol scheme is special.
 *
 * @param {String} The protocol scheme of the URL
 * @return {Boolean} `true` if the protocol scheme is special, else `false`
 * @private
 */function isSpecial(e){return"file:"===e||"ftp:"===e||"http:"===e||"https:"===e||"ws:"===e||"wss:"===e}
/**
 * @typedef ProtocolExtract
 * @type Object
 * @property {String} protocol Protocol matched in the URL, in lowercase.
 * @property {Boolean} slashes `true` if protocol is followed by "//", else `false`.
 * @property {String} rest Rest of the URL that is not part of the protocol.
 */
/**
 * Extract protocol information from a URL with/without double slash ("//").
 *
 * @param {String} address URL we want to extract from.
 * @param {Object} location
 * @return {ProtocolExtract} Extracted information.
 * @private
 */function extractProtocol(e,o){e=trimLeft(e);e=e.replace(p,"");o=o||{};var t=h.exec(e);var s=t[1]?t[1].toLowerCase():"";var r=!!t[2];var a=!!t[3];var n=0;var l;if(r)if(a){l=t[2]+t[3]+t[4];n=t[2].length+t[3].length}else{l=t[2]+t[4];n=t[2].length}else if(a){l=t[3]+t[4];n=t[3].length}else l=t[4];"file:"===s?n>=2&&(l=l.slice(2)):isSpecial(s)?l=t[4]:s?r&&(l=l.slice(2)):n>=2&&isSpecial(o.protocol)&&(l=t[4]);return{protocol:s,slashes:r||isSpecial(s),slashesCount:n,rest:l}}
/**
 * Resolve a relative URL pathname against a base URL pathname.
 *
 * @param {String} relative Pathname of the relative URL.
 * @param {String} base Pathname of the base URL.
 * @return {String} Resolved pathname.
 * @private
 */function resolve(e,o){if(""===e)return o;var t=(o||"/").split("/").slice(0,-1).concat(e.split("/")),s=t.length,r=t[s-1],a=false,n=0;while(s--)if("."===t[s])t.splice(s,1);else if(".."===t[s]){t.splice(s,1);n++}else if(n){0===s&&(a=true);t.splice(s,1);n--}a&&t.unshift("");"."!==r&&".."!==r||t.push("");return t.join("/")}
/**
 * The actual URL instance. Instead of returning an object we've opted-in to
 * create an actual constructor as it's much more memory efficient and
 * faster and it pleases my OCD.
 *
 * It is worth noting that we should not use `URL` as class name to prevent
 * clashes with the global URL instance that got introduced in browsers.
 *
 * @constructor
 * @param {String} address URL we want to parse.
 * @param {Object|String} [location] Location defaults for relative paths.
 * @param {Boolean|Function} [parser] Parser for the query string.
 * @private
 */function Url(e,o,t){e=trimLeft(e);e=e.replace(p,"");if(!(this instanceof Url))return new Url(e,o,t);var s,r,a,i,c,f,h=d.slice(),m=typeof o,v=this,w=0;if("object"!==m&&"string"!==m){t=o;o=null}t&&"function"!==typeof t&&(t=l.parse);o=lolcation(o);r=extractProtocol(e||"",o);s=!r.protocol&&!r.slashes;v.slashes=r.slashes||s&&o.slashes;v.protocol=r.protocol||o.protocol||"";e=r.rest;("file:"===r.protocol&&(2!==r.slashesCount||u.test(e))||!r.slashes&&(r.protocol||r.slashesCount<2||!isSpecial(v.protocol)))&&(h[3]=[/(.*)/,"pathname"]);for(;w<h.length;w++){i=h[w];if("function"!==typeof i){a=i[0];f=i[1];if(a!==a)v[f]=e;else if("string"===typeof a){c="@"===a?e.lastIndexOf(a):e.indexOf(a);if(~c)if("number"===typeof i[2]){v[f]=e.slice(0,c);e=e.slice(c+i[2])}else{v[f]=e.slice(c);e=e.slice(0,c)}}else if(c=a.exec(e)){v[f]=c[1];e=e.slice(0,c.index)}v[f]=v[f]||s&&i[3]&&o[f]||"";i[4]&&(v[f]=v[f].toLowerCase())}else e=i(e,v)}t&&(v.query=t(v.query));s&&o.slashes&&"/"!==v.pathname.charAt(0)&&(""!==v.pathname||""!==o.pathname)&&(v.pathname=resolve(v.pathname,o.pathname));"/"!==v.pathname.charAt(0)&&isSpecial(v.protocol)&&(v.pathname="/"+v.pathname);if(!n(v.port,v.protocol)){v.host=v.hostname;v.port=""}v.username=v.password="";if(v.auth){c=v.auth.indexOf(":");if(~c){v.username=v.auth.slice(0,c);v.username=encodeURIComponent(decodeURIComponent(v.username));v.password=v.auth.slice(c+1);v.password=encodeURIComponent(decodeURIComponent(v.password))}else v.username=encodeURIComponent(decodeURIComponent(v.auth));v.auth=v.password?v.username+":"+v.password:v.username}v.origin="file:"!==v.protocol&&isSpecial(v.protocol)&&v.host?v.protocol+"//"+v.host:"null";v.href=v.toString()}
/**
 * This is convenience method for changing properties in the URL instance to
 * insure that they all propagate correctly.
 *
 * @param {String} part          Property we need to adjust.
 * @param {Mixed} value          The newly assigned value.
 * @param {Boolean|Function} fn  When setting the query, it will be the function
 *                               used to parse the query.
 *                               When setting the protocol, double slash will be
 *                               removed from the final url if it is true.
 * @returns {URL} URL instance for chaining.
 * @public
 */function set(e,o,t){var s=this;switch(e){case"query":"string"===typeof o&&o.length&&(o=(t||l.parse)(o));s[e]=o;break;case"port":s[e]=o;if(n(o,s.protocol))o&&(s.host=s.hostname+":"+o);else{s.host=s.hostname;s[e]=""}break;case"hostname":s[e]=o;s.port&&(o+=":"+s.port);s.host=o;break;case"host":s[e]=o;if(f.test(o)){o=o.split(":");s.port=o.pop();s.hostname=o.join(":")}else{s.hostname=o;s.port=""}break;case"protocol":s.protocol=o.toLowerCase();s.slashes=!t;break;case"pathname":case"hash":if(o){var r="pathname"===e?"/":"#";s[e]=o.charAt(0)!==r?r+o:o}else s[e]=o;break;case"username":case"password":s[e]=encodeURIComponent(o);break;case"auth":var a=o.indexOf(":");if(~a){s.username=o.slice(0,a);s.username=encodeURIComponent(decodeURIComponent(s.username));s.password=o.slice(a+1);s.password=encodeURIComponent(decodeURIComponent(s.password))}else s.username=encodeURIComponent(decodeURIComponent(o))}for(var i=0;i<d.length;i++){var p=d[i];p[4]&&(s[p[1]]=s[p[1]].toLowerCase())}s.auth=s.password?s.username+":"+s.password:s.username;s.origin="file:"!==s.protocol&&isSpecial(s.protocol)&&s.host?s.protocol+"//"+s.host:"null";s.href=s.toString();return s}
/**
 * Transform the properties back in to a valid and full URL string.
 *
 * @param {Function} stringify Optional query stringify function.
 * @returns {String} Compiled version of the URL.
 * @public
 */function toString(e){e&&"function"===typeof e||(e=l.stringify);var o,t=this,s=t.host,r=t.protocol;r&&":"!==r.charAt(r.length-1)&&(r+=":");var a=r+(t.protocol&&t.slashes||isSpecial(t.protocol)?"//":"");if(t.username){a+=t.username;t.password&&(a+=":"+t.password);a+="@"}else if(t.password){a+=":"+t.password;a+="@"}else"file:"!==t.protocol&&isSpecial(t.protocol)&&!s&&"/"!==t.pathname&&(a+="@");(":"===s[s.length-1]||f.test(t.hostname)&&!t.port)&&(s+=":");a+=s+t.pathname;o="object"===typeof t.query?e(t.query):t.query;o&&(a+="?"!==o.charAt(0)?"?"+o:o);t.hash&&(a+=t.hash);return a}Url.prototype={set:set,toString:toString};Url.extractProtocol=extractProtocol;Url.location=lolcation;Url.trimLeft=trimLeft;Url.qs=l;a=Url;var v=a;export{v as default};

