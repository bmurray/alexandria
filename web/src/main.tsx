import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import './styles/global.css'
import './styles/catalog.css'
import './styles/book.css'
import './styles/forms.css'
import './styles/tables.css'
import './styles/drawer.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
