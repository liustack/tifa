import { Link } from '@tanstack/react-router'
import { PencilLine } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { cn } from 'cn'
import { yuan } from '../../../format'
import { photo } from '../../../store/images'
import { layout as projectRoute, type LayoutData as Project } from '../route.gen'
import { page } from './route.gen'

const th = 'text-xs font-normal text-muted-foreground'

// lowStock is the stock under which the list marks a product, so staff can order more in time.
const lowStock = 10

export default function ProductsPage() {
  const project = projectRoute.useLoaderData()
  const products = page.useLoaderData()
  const soldOut = products.filter((p) => p.stock === 0).length
  const low = products.filter((p) => p.stock > 0 && p.stock < lowStock).length
  return (
    <section aria-labelledby="products-title" className="flex flex-col gap-6">
      <div>
        <h2 id="products-title" className="font-heading text-xl font-medium tracking-[0.04em]">
          Products
        </h2>
        <p className="text-sm text-muted-foreground">What the storefront sells. A change shows in the storefront at once.</p>
      </div>
      <Card>
        <CardHeader>
          <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">Catalogue</CardTitle>
          <CardDescription>
            {soldOut > 0 ? `${soldOut} sold out` : 'Nothing sold out'}
            {low > 0 && ` · ${low} under ${lowStock} in stock`}
          </CardDescription>
          <CardAction className="font-mono text-xs text-muted-foreground">{products.length} products</CardAction>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className={cn(th, 'w-12')}>No.</TableHead>
                <TableHead className={th}>Product</TableHead>
                <TableHead className={th}>Category</TableHead>
                <TableHead className={cn(th, 'text-right')}>Price</TableHead>
                <TableHead className={cn(th, 'text-right')}>Stock</TableHead>
                <TableHead className={cn(th, 'w-10')}>
                  <span className="sr-only">Edit</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {products.map((p) => (
                <TableRow key={p.slug}>
                  <TableCell className="font-mono text-xs text-muted-foreground">{String(p.number).padStart(3, '0')}</TableCell>
                  <TableCell>
                    <Link
                      to="/projects/$project/products/$product"
                      params={{ project: project.id, product: p.slug }}
                      className="flex items-center gap-3 hover:underline"
                    >
                      <img
                        src={photo(p.photo, 'main', 160)}
                        alt=""
                        width={40}
                        height={40}
                        loading="lazy"
                        decoding="async"
                        className="size-10 bg-[#d3dbe2] object-cover dark:bg-[#273240]"
                      />
                      <span className="flex flex-col">
                        <span className="text-sm">{p.nameEn}</span>
                        <span className="text-xs text-muted-foreground">{p.nameZh}</span>
                      </span>
                    </Link>
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">{p.category}</TableCell>
                  <TableCell className="text-right font-mono tabular-nums">{yuan(p.priceCents)}</TableCell>
                  <TableCell className="text-right">
                    {p.stock === 0 ? (
                      <Badge variant="outline" className="rounded-none font-normal text-destructive">
                        Sold out
                      </Badge>
                    ) : (
                      <span className={cn('font-mono tabular-nums', p.stock < lowStock && 'text-destructive')}>{p.stock}</span>
                    )}
                  </TableCell>
                  <TableCell>
                    <Button variant="ghost" size="icon" asChild>
                      <Link to="/projects/$project/products/$product" params={{ project: project.id, product: p.slug }}>
                        <PencilLine />
                        <span className="sr-only">Edit {p.nameEn}</span>
                      </Link>
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </section>
  )
}

export const head = ({ matches }: { matches: Array<{ routeId: string; loaderData?: unknown }> }) => {
  const project = matches.find((m) => m.routeId === projectRoute.id)?.loaderData as Project | undefined
  return { meta: project ? [{ title: `Products · ${project.name} · Shanhai Workbench` }] : [] }
}
