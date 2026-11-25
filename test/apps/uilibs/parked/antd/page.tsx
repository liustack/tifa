// Parked: any import of antd makes `tifa build` run for ever (the small-function pass of the SSR
// bundle, runsAfter in js/vite/plugin.js, is exponential on antd's code). Move this folder to
// app/antd to reproduce. See TestUILibsApp.
import { useState } from 'react'
import 'antd/dist/antd.css'
import { Button, ConfigProvider, Input, Modal, Select, Tabs } from 'antd'

export const head = () => ({ meta: [{ title: 'Ant Design' }] })

export default function AntdPage() {
  const [open, setOpen] = useState(false)
  return (
    <ConfigProvider theme={{ zeroRuntime: true }}>
      <main style={{ padding: 24 }}>
        <h1>Ant Design</h1>
        <Button type="primary" onClick={() => setOpen(true)}>
          Open modal
        </Button>
        <Input placeholder="Your name" />
        <Select
          aria-label="Fruit"
          defaultValue="b"
          style={{ width: 160 }}
          options={[
            { value: 'a', label: 'Apple' },
            { value: 'b', label: 'Banana' },
          ]}
        />
        <Tabs
          defaultActiveKey="one"
          items={[
            { key: 'one', label: 'One', children: 'Tab one selected' },
            { key: 'two', label: 'Two', children: 'Tab two selected' },
          ]}
        />
        <Modal open={open} title="Hello" onCancel={() => setOpen(false)} onOk={() => setOpen(false)}>
          Press Esc to close.
        </Modal>
      </main>
    </ConfigProvider>
  )
}
