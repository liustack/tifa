// A head that returns a promise, as a lazy route module's does in the browser. null takes away an
// attribute an outer level set, and the attributes React DOM would not write are left out.
export const head = async () => ({
  meta: [{ title: 'RTL' }],
  htmlAttributes: { dir: 'rtl', className: 'wide', 'data-theme': null, onClick: 'alert(1)', 'bad name': 'x', title: 'a"<b' },
})

export default function Rtl() {
  return <p>INTL_RTL</p>
}
