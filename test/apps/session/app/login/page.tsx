import { useState } from 'react'
import { POST } from './route.gen'

export default function Login() {
  const [name, setName] = useState('')
  return (
    <form
      id="login"
      onSubmit={(event) => {
        event.preventDefault()
        void POST({ input: { name } })
      }}
    >
      <input id="name" value={name} onChange={(event) => setName(event.target.value)} />
      <button type="submit">Sign in</button>
    </form>
  )
}
