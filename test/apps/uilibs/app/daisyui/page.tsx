import { useRef, useState } from 'react'
import './daisy.css'

export const head = () => ({ meta: [{ title: 'daisyUI' }] })

export default function DaisyPage() {
  const dialog = useRef<HTMLDialogElement>(null)
  const [tab, setTab] = useState('one')
  const [name, setName] = useState('')
  return (
    <main className="p-6 space-y-4" data-theme="light">
      <h1 className="text-2xl font-bold">daisyUI</h1>
      <button className="btn btn-primary" onClick={() => dialog.current?.showModal()}>
        Open dialog
      </button>
      <input className="input input-bordered" placeholder="Your name" value={name} onChange={(e) => setName(e.target.value)} />
      <select className="select select-bordered" defaultValue="b" aria-label="Fruit">
        <option value="a">Apple</option>
        <option value="b">Banana</option>
      </select>
      <div role="tablist" className="tabs tabs-border">
        <button role="tab" className={'tab' + (tab === 'one' ? ' tab-active' : '')} onClick={() => setTab('one')}>
          One
        </button>
        <button role="tab" className={'tab' + (tab === 'two' ? ' tab-active' : '')} onClick={() => setTab('two')}>
          Two
        </button>
      </div>
      <p role="tabpanel">Tab {tab} selected</p>
      <dialog ref={dialog} className="modal">
        <div className="modal-box">
          <h3 className="text-lg font-bold">Hello</h3>
          <p>Press Esc or click outside to close.</p>
          <form method="dialog" className="modal-action">
            <button className="btn">Close</button>
          </form>
        </div>
        <form method="dialog" className="modal-backdrop">
          <button>close</button>
        </form>
      </dialog>
    </main>
  )
}
