// requires-port@1.0.0 downloaded from https://ga.jspm.io/npm:requires-port@1.0.0/index.js

var e={};e=function required(e,r){r=r.split(":")[0];e=+e;if(!e)return false;switch(r){case"http":case"ws":return 80!==e;case"https":case"wss":return 443!==e;case"ftp":return 21!==e;case"gopher":return 70!==e;case"file":return false}return 0!==e};var r=e;export default r;

