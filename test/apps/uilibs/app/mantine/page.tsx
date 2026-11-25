import '@mantine/core/styles.css'
import { Button, MantineProvider, Modal, Select, Tabs, TextInput } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'

// Mantine's own SSR setup is a ColorSchemeScript in the head. Tifa writes the head itself, so the
// attribute that script would set goes in htmlAttributes. Without it every color variable of the
// stylesheet (keyed on this attribute) is undefined until the provider sets it in an effect.
export const head = () => ({ meta: [{ title: 'Mantine' }], htmlAttributes: { 'data-mantine-color-scheme': 'light' } })

export default function MantinePage() {
  const [opened, { open, close }] = useDisclosure(false)
  return (
    <MantineProvider defaultColorScheme="light">
      <main style={{ padding: 24 }}>
        <h1>Mantine</h1>
        <Button onClick={open}>Open modal</Button>
        <TextInput label="Name" placeholder="Your name" />
        <Select label="Fruit" data={['Apple', 'Banana']} defaultValue="Banana" />
        <Tabs defaultValue="one">
          <Tabs.List>
            <Tabs.Tab value="one">One</Tabs.Tab>
            <Tabs.Tab value="two">Two</Tabs.Tab>
          </Tabs.List>
          <Tabs.Panel value="one">Tab one selected</Tabs.Panel>
          <Tabs.Panel value="two">Tab two selected</Tabs.Panel>
        </Tabs>
        <Modal opened={opened} onClose={close} title="Hello">
          Press Esc to close.
        </Modal>
      </main>
    </MantineProvider>
  )
}
