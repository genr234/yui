package blocker

const defaultRuleList = `
! Compact uBlock/EasyList-style built-in rules for YUI embeds.
! The engine intentionally supports the common host/subresource subset used here.
||2mdn.net^
||adform.net^
||adnxs.com^
||adsafeprotected.com^
||adsrvr.org^
||amazon-adsystem.com^
||analytics.google.com^
||app-measurement.com^
||bing.com^/bat.js
||chartbeat.com^
||criteo.com^
||criteo.net^
||doubleclick.net^
||facebook.com^/tr/
||facebook.net^
||google-analytics.com^
||googleadservices.com^
||googlesyndication.com^
||googletagmanager.com^/gtag/js
||googletagservices.com^
||hotjar.com^
||moatads.com^
||outbrain.com^
||scorecardresearch.com^
||taboola.com^
||youtube.com^/pagead/
||ytimg.com^/ptracking
/adsystem/
/adservice/
/pagead/
/pagead2.
/partnerads/
/prebid.
/pubads_
/pubads.
/securepubads.
/trackers/
/tracking/
`
