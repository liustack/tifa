import { StoreRootError } from '../../../compare/store/entries'

// A language the storefront does not have. Its Layout failed, so the visitor's language is not
// known: the page speaks both, as examples/shop/app/store/layout_.tsx does.
export default function NotFound() {
  return <StoreRootError />
}
