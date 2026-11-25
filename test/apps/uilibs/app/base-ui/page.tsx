import { Button } from '@base-ui/react/button'
import { Dialog } from '@base-ui/react/dialog'
import { Input } from '@base-ui/react/input'
import { Select } from '@base-ui/react/select'
import { Tabs } from '@base-ui/react/tabs'
import './base.css'

export const head = () => ({ meta: [{ title: 'Base UI' }] })

const fruits = [
  { value: 'a', label: 'Apple' },
  { value: 'b', label: 'Banana' },
]

export default function BaseUIPage() {
  return (
    <main>
      <h1>Base UI</h1>
      <Button className="bu-button">Plain button</Button>
      <Input className="bu-input" placeholder="Your name" />
      <Select.Root items={fruits} defaultValue="b">
        <Select.Trigger className="bu-input" aria-label="Fruit">
          <Select.Value />
        </Select.Trigger>
        <Select.Portal>
          <Select.Positioner>
            <Select.Popup className="bu-popup">
              {fruits.map((f) => (
                <Select.Item key={f.value} value={f.value}>
                  <Select.ItemText>{f.label}</Select.ItemText>
                </Select.Item>
              ))}
            </Select.Popup>
          </Select.Positioner>
        </Select.Portal>
      </Select.Root>
      <Tabs.Root defaultValue="one">
        <Tabs.List>
          <Tabs.Tab className="bu-tab" value="one">One</Tabs.Tab>
          <Tabs.Tab className="bu-tab" value="two">Two</Tabs.Tab>
        </Tabs.List>
        <Tabs.Panel value="one">Tab one selected</Tabs.Panel>
        <Tabs.Panel value="two">Tab two selected</Tabs.Panel>
      </Tabs.Root>
      <Dialog.Root>
        <Dialog.Trigger className="bu-button">Open dialog</Dialog.Trigger>
        <Dialog.Portal>
          <Dialog.Backdrop className="bu-backdrop" />
          <Dialog.Popup className="bu-dialog">
            <Dialog.Title>Hello</Dialog.Title>
            <Dialog.Description>Press Esc to close.</Dialog.Description>
            <Dialog.Close className="bu-button">Close</Dialog.Close>
          </Dialog.Popup>
        </Dialog.Portal>
      </Dialog.Root>
    </main>
  )
}
