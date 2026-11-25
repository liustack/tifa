import { Button, ChakraProvider, Dialog, Input, NativeSelect, Portal, Select, Tabs, createListCollection, defaultSystem } from '@chakra-ui/react'

const fruits = createListCollection({
  items: [
    { value: 'a', label: 'Apple' },
    { value: 'b', label: 'Banana' },
  ],
})

export const head = () => ({ meta: [{ title: 'Chakra UI' }] })

export default function ChakraPage() {
  return (
    <ChakraProvider value={defaultSystem}>
      <main style={{ padding: 24 }}>
        <h1>Chakra UI</h1>
        <Button colorPalette="blue">Plain button</Button>
        <Input placeholder="Your name" />
        <NativeSelect.Root>
          <NativeSelect.Field defaultValue="b" aria-label="Fruit">
            <option value="a">Apple</option>
            <option value="b">Banana</option>
          </NativeSelect.Field>
          <NativeSelect.Indicator />
        </NativeSelect.Root>
        <Select.Root collection={fruits} defaultValue={['b']} width="200px">
          <Select.HiddenSelect />
          <Select.Control>
            <Select.Trigger>
              <Select.ValueText placeholder="Pick a fruit" />
            </Select.Trigger>
            <Select.IndicatorGroup>
              <Select.Indicator />
            </Select.IndicatorGroup>
          </Select.Control>
          <Portal>
            <Select.Positioner>
              <Select.Content>
                {fruits.items.map((f) => (
                  <Select.Item item={f} key={f.value}>
                    {f.label}
                    <Select.ItemIndicator />
                  </Select.Item>
                ))}
              </Select.Content>
            </Select.Positioner>
          </Portal>
        </Select.Root>
        <Tabs.Root defaultValue="one">
          <Tabs.List>
            <Tabs.Trigger value="one">One</Tabs.Trigger>
            <Tabs.Trigger value="two">Two</Tabs.Trigger>
          </Tabs.List>
          <Tabs.Content value="one">Tab one selected</Tabs.Content>
          <Tabs.Content value="two">Tab two selected</Tabs.Content>
        </Tabs.Root>
        <Dialog.Root>
          <Dialog.Trigger asChild>
            <Button>Open dialog</Button>
          </Dialog.Trigger>
          <Portal>
            <Dialog.Backdrop />
            <Dialog.Positioner>
              <Dialog.Content>
                <Dialog.Header>
                  <Dialog.Title>Hello</Dialog.Title>
                </Dialog.Header>
                <Dialog.Body>Press Esc to close.</Dialog.Body>
                <Dialog.CloseTrigger asChild>
                  <Button>Close</Button>
                </Dialog.CloseTrigger>
              </Dialog.Content>
            </Dialog.Positioner>
          </Portal>
        </Dialog.Root>
      </main>
    </ChakraProvider>
  )
}
