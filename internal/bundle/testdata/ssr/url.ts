import 'core-js/web/url.js'

globalThis.result = new URL('/orders?id=SH-1042', 'http://shop.invalid').searchParams.get('id')
