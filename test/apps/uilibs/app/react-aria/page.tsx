import {
  Button,
  Dialog,
  DialogTrigger,
  Heading,
  Input,
  Label,
  ListBox,
  ListBoxItem,
  Modal,
  ModalOverlay,
  Popover,
  Select,
  SelectValue,
  Tab,
  TabList,
  TabPanel,
  Tabs,
  TextField,
} from 'react-aria-components'
import './aria.css'

export const head = () => ({ meta: [{ title: 'React Aria Components' }] })

export default function AriaPage() {
  return (
    <main>
      <h1>React Aria Components</h1>
      <Button className="ra-button">Plain button</Button>
      <TextField>
        <Label>Name</Label>
        <Input className="ra-input" placeholder="Your name" />
      </TextField>
      <Select defaultValue="b">
        <Label>Fruit</Label>
        <Button className="ra-button">
          <SelectValue />
        </Button>
        <Popover className="ra-popover">
          <ListBox>
            <ListBoxItem id="a">Apple</ListBoxItem>
            <ListBoxItem id="b">Banana</ListBoxItem>
          </ListBox>
        </Popover>
      </Select>
      <Tabs>
        <TabList aria-label="Sections">
          <Tab className="ra-tab" id="one">One</Tab>
          <Tab className="ra-tab" id="two">Two</Tab>
        </TabList>
        <TabPanel id="one">Tab one selected</TabPanel>
        <TabPanel id="two">Tab two selected</TabPanel>
      </Tabs>
      <DialogTrigger>
        <Button className="ra-button">Open dialog</Button>
        <ModalOverlay className="ra-overlay" isDismissable>
          <Modal className="ra-modal">
            <Dialog>
              {({ close }) => (
                <>
                  <Heading slot="title">Hello</Heading>
                  <p>Press Esc to close.</p>
                  <Button className="ra-button" onPress={close}>Close</Button>
                </>
              )}
            </Dialog>
          </Modal>
        </ModalOverlay>
      </DialogTrigger>
    </main>
  )
}
