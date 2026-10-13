import './host'
import api from 'react-dom/server.edge'

globalThis.result = Object.keys(api).sort().join(',')
