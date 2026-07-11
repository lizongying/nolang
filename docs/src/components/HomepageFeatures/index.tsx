import type {ReactNode} from 'react';
import clsx from 'clsx';
import Heading from '@theme/Heading';
import {translate} from '@docusaurus/Translate';
import styles from './styles.module.css';

type FeatureItem = {
  title: string;
  // Svg: React.ComponentType<React.ComponentProps<'svg'>>;
  description: ReactNode;
};

const FeatureList: FeatureItem[] = [
  {
    title: translate({message: '無 GC'}),
    // Svg: require('@site/static/img/0.svg').default,
    description: (
      <>
        {translate({message: '不依賴垃圾回收，自动安全內存管理'})}
      </>
    ),
  },
  {
    title: translate({message: '內存安全'}),
    // Svg: require('@site/static/img/0.svg').default,
    description: (
      <>
        {translate({message: '作用域離開自動釋放，杜絕懸垂引用、內存泄漏'})}
      </>
    ),
  },
  {
    title: translate({message: '語法極簡'}),
    // Svg: require('@site/static/img/0.svg').default,
    description: (
      <>
        {translate({message: '极少關鍵字，極簡語法'})}
      </>
    ),
  },
];

function Feature({title, description}: FeatureItem) {
  return (
    <div className={clsx('col col--4')}>
      <div className="text--center">
        {/* <Svg className={styles.featureSvg} role="img" /> */}
      </div>
      <div className="text--center padding-horiz--md">
        <Heading as="h3">{title}</Heading>
        {/* <p>{description}</p> */}
      </div>
    </div>
  );
}

export default function HomepageFeatures(): ReactNode {
  return (
    <section className={styles.features}>
      <div className="container">
        <div className="row">
          {FeatureList.map((props, idx) => (
            <Feature key={idx} {...props} />
          ))}
        </div>
      </div>
    </section>
  );
}
