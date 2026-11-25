// antd 6 without its runtime styles (cssinjs writes them nowhere Tifa can reach): import
// antd/dist/antd.css and turn zeroRuntime on. The color variables in that file hang on a class
// made from the useId value of antd's own build, css-var-_R_0_. ConfigProvider would make its
// own class from useId, which differs, so the variables stay undefined until a script runs.
// cssVar.key gives it the class the file uses. A new antd may name that class otherwise:
// TestUILibsApp then fails and the key here is to be changed to the one in antd.css.
import { useState } from 'react'
import 'antd/dist/antd.css'
import { Button, ConfigProvider, Input, Modal, Select, Tabs } from 'antd'

export const head = () => ({ meta: [{ title: 'Ant Design' }] })

export default function AntdPage() {
  const [open, setOpen] = useState(false)
  return (
    <ConfigProvider theme={{ zeroRuntime: true, cssVar: { key: 'css-var-_R_0_' } }}>
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
