import { createContext } from 'react'

/** True while the catalog socket is connected. */
export const LiveContext = createContext<boolean>(false)
