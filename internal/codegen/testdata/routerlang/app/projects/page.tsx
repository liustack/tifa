import { Link } from '@tanstack/react-router'

export default function Page() {
  return (
    <main>
      <p>PAGE_PROJECTS</p>
      <Link to="/projects">to projects</Link>
      <Link to="/store/cart">to cart</Link>
    </main>
  )
}
