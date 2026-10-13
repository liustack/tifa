import './host'
import * as api from 'react-dom/server.edge'

globalThis.result = Object.keys(api)
  .filter((key) => key !== 'default')
  .sort()
  .join(',')
