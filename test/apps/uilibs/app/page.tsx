const pages = ['/daisyui', '/base-ui', '/mantine', '/chakra', '/styled', '/react-aria']

export const head = () => ({ meta: [{ title: 'UI libraries' }] })

export default function Home() {
  return (
    <ul>
      {pages.map((p) => (
        <li key={p}>
          <a href={p}>{p}</a>
        </li>
      ))}
    </ul>
  )
}
